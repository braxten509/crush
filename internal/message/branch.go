package message

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf16"

	"github.com/charmbracelet/crush/internal/db"
	"github.com/google/uuid"
)

// BranchRequest asks for a branch: a new chat copied from SessionID through
// the message holding the highlight (engine-design.md 5.11).
type BranchRequest struct {
	SessionID      string // source chat
	MessageID      string // the message holding the highlight (user or assistant)
	HighlightText  string
	HighlightStart int // UTF-16 code units into the message's text
	HighlightEnd   int
	Title          string
}

// BranchResult describes the branch chat Branch made.
type BranchResult struct {
	SessionID     string // the new chat
	LinkID        string
	CopiedThrough string // last source message copied
	Copied        int
}

// BranchService makes branch chats. It is optional, so existing upstream
// stores and mocks remain valid; the message service implements it.
type BranchService interface {
	Branch(ctx context.Context, req BranchRequest) (BranchResult, error)
}

var (
	ErrBadCut         = errors.New("bad cut")
	ErrCutNotFinished = errors.New("cut not finished")
	// The copy checks it in its own transaction (db.CopyBranch).
	ErrHighlightOverlapsLink = db.ErrHighlightOverlapsLink
)

// BranchOrigins reports whether a chat is a branch. The message service
// implements it by type-asserting its queries (s.q) to an interface with
// db.Queries.BranchOrigin, as Tree does with treeStorage.
type BranchOrigins interface {
	BranchOrigin(ctx context.Context, sessionID string) (link db.ChatLink, ok bool, err error)
}

// MarkerRecorder saves an AI tool's own ID for a Crush message, so a branch
// can fork the tool's memory at its cut. The message service implements it
// through its queries, as it does BranchOrigins.
type MarkerRecorder interface {
	RecordMarker(ctx context.Context, m db.MessageMarker) error
}

// branchError refuses a branch. Its text is shown as is, and errors.Is
// matches its kind (ErrBadCut or ErrCutNotFinished).
type branchError struct {
	kind error
	msg  string
}

func (e branchError) Error() string        { return e.msg }
func (e branchError) Is(target error) bool { return target == e.kind }

type branchStorage interface {
	CopyBranch(context.Context, db.BranchCopy) (map[string]string, error)
	BranchOrigin(context.Context, string) (db.ChatLink, bool, error)
	ChatLinks(context.Context) ([]db.ChatLink, error)
	TreeAncestry(context.Context, string, string) ([]string, error)
	TreeNodes(context.Context, string) ([]db.TreeNode, error)
}

// maxBranchTitle is how many characters of a branch's title are stored.
const maxBranchTitle = 200

// Branch copies the source chat through the highlighted message into a new
// top-level chat, with the extras every copy brings, and links the two. It
// never restores files, moves the undo record, writes to the source chat,
// touches a link to an AI tool or checks whether the source chat is busy:
// a reply running in the source carries on undisturbed. The caller
// announces the new chat.
func (s *service) Branch(ctx context.Context, req BranchRequest) (BranchResult, error) {
	storage, ok := s.q.(branchStorage)
	if !ok {
		return BranchResult{}, errors.New("branch storage unavailable")
	}
	// Pending writes of finished messages land first. The running reply is
	// not waited for: it is always below the cut.
	if err := s.FlushAll(ctx); err != nil {
		return BranchResult{}, err
	}
	m, err := s.load(ctx, req.MessageID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && m.SessionID != req.SessionID) {
		return BranchResult{}, branchError{ErrBadCut, "the message belongs to another chat"}
	}
	if err != nil {
		return BranchResult{}, err
	}
	if m.Role != User && m.Role != Assistant {
		return BranchResult{}, branchError{ErrBadCut, "branch from message text, not tool output"}
	}
	if !highlightMatches(m.Content().Text, req.HighlightText, req.HighlightStart, req.HighlightEnd) {
		return BranchResult{}, branchError{ErrBadCut, "the highlight doesn't match the message"}
	}
	if m.Role == Assistant && !m.IsFinished() {
		return BranchResult{}, branchError{ErrCutNotFinished, "this reply is still being written"}
	}
	path, err := storage.TreeAncestry(ctx, req.SessionID, m.ID)
	if err != nil {
		return BranchResult{}, err
	}
	if len(path) == 0 {
		return BranchResult{}, branchError{ErrBadCut, "the message belongs to another chat"}
	}
	results, err := s.answeringTools(ctx, storage, req.SessionID, m)
	if err != nil {
		return BranchResult{}, err
	}
	path = append(path, results...)
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return BranchResult{}, branchError{ErrBadCut, "the branch needs a title"}
	}
	if r := []rune(title); len(r) > maxBranchTitle {
		title = string(r[:maxBranchTitle])
	}
	newID := uuid.NewString()
	if _, err := storage.CopyBranch(ctx, db.BranchCopy{
		Source:     req.SessionID,
		MessageIDs: path,
		NewID:      newID,
		Title:      title,
		Origin: db.ChatLink{
			ParentSessionID: req.SessionID,
			SourceMessageID: m.ID,
			HighlightText:   req.HighlightText,
			HighlightStart:  req.HighlightStart,
			HighlightEnd:    req.HighlightEnd,
		},
	}); err != nil {
		return BranchResult{}, err
	}
	origin, _, err := storage.BranchOrigin(ctx, newID)
	if err != nil {
		return BranchResult{}, err
	}
	return BranchResult{SessionID: newID, LinkID: origin.ID, CopiedThrough: path[len(path)-1], Copied: len(path)}, nil
}

// highlightMatches reports whether text[start:end], measured in UTF-16 code
// units as the desktop app measures it, is the highlighted text.
func highlightMatches(text, highlight string, start, end int) bool {
	units := utf16.Encode([]rune(text))
	if start < 0 || start >= end || end > len(units) {
		return false
	}
	return string(utf16.Decode(units[start:end])) == highlight
}

// answeringTools lists, in path order, the tool messages under m whose
// results answer m's tool calls. When m has more than one such child, the
// newest is taken. A call with no result yet refuses the cut.
func (s *service) answeringTools(ctx context.Context, storage branchStorage, sessionID string, m Message) ([]string, error) {
	pending := map[string]bool{}
	for _, call := range m.ToolCalls() {
		if !call.ProviderExecuted {
			pending[call.ID] = true
		}
	}
	if len(pending) == 0 {
		return nil, nil
	}
	nodes, err := storage.TreeNodes(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	// Nodes come oldest first, so each list of children does too.
	children := map[string][]string{}
	for _, n := range nodes {
		children[n.ParentID] = append(children[n.ParentID], n.MessageID)
	}
	var ids []string
	for current := m.ID; len(pending) > 0; {
		next := ""
		kids := children[current]
		for i := len(kids) - 1; i >= 0 && next == ""; i-- {
			child, err := s.load(ctx, kids[i])
			if err != nil {
				return nil, err
			}
			if child.Role != Tool || !answers(child, pending) {
				continue
			}
			for _, result := range child.ToolResults() {
				delete(pending, result.ToolCallID)
			}
			next = child.ID
		}
		if next == "" {
			break
		}
		ids = append(ids, next)
		current = next
	}
	if len(pending) > 0 {
		return nil, branchError{ErrCutNotFinished, "this reply's tools are still running"}
	}
	return ids, nil
}

func answers(m Message, pending map[string]bool) bool {
	for _, result := range m.ToolResults() {
		if pending[result.ToolCallID] {
			return true
		}
	}
	return false
}

// load reads a message as saved, without its retained file review.
func (s *service) load(ctx context.Context, id string) (Message, error) {
	item, err := s.q.GetMessage(ctx, id)
	if err != nil {
		return Message{}, err
	}
	return s.fromDBItem(item)
}

// BranchOrigin implements BranchOrigins. A store without links reports that
// no chat is a branch.
func (s *service) BranchOrigin(ctx context.Context, sessionID string) (db.ChatLink, bool, error) {
	storage, ok := s.q.(interface {
		BranchOrigin(context.Context, string) (db.ChatLink, bool, error)
	})
	if !ok {
		return db.ChatLink{}, false, nil
	}
	return storage.BranchOrigin(ctx, sessionID)
}

// ChatLinks lists every link between chats, oldest first.
func (s *service) ChatLinks(ctx context.Context) ([]db.ChatLink, error) {
	storage, ok := s.q.(interface {
		ChatLinks(context.Context) ([]db.ChatLink, error)
	})
	if !ok {
		return nil, errors.New("branch storage unavailable")
	}
	return storage.ChatLinks(ctx)
}

// RecordMarker implements MarkerRecorder.
func (s *service) RecordMarker(ctx context.Context, m db.MessageMarker) error {
	storage, ok := s.q.(interface {
		RecordMarker(context.Context, db.MessageMarker) error
	})
	if !ok {
		return errors.New("marker storage unavailable")
	}
	return storage.RecordMarker(ctx, m)
}

// MarkerReader lists the AI-tool markers recorded on a chat's messages,
// oldest first, so a branch can find where to fork a tool's memory.
type MarkerReader interface {
	Markers(ctx context.Context, sessionID string) ([]db.MessageMarker, error)
}

// Markers implements MarkerReader.
func (s *service) Markers(ctx context.Context, sessionID string) ([]db.MessageMarker, error) {
	storage, ok := s.q.(interface {
		MarkersForSession(context.Context, string) ([]db.MessageMarker, error)
	})
	if !ok {
		return nil, errors.New("marker storage unavailable")
	}
	return storage.MarkersForSession(ctx, sessionID)
}
