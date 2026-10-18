package proto

import (
	"encoding/json"

	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/filehistory"
	"github.com/charmbracelet/crush/internal/message"
)

// Tree actions (engine-design.md 5.6).

// TreeEntry is one message of a chat's tree, with a text preview.
type TreeEntry struct {
	MessageID string  `json:"message_id"`
	ParentID  string  `json:"parent_id"`
	Label     string  `json:"label,omitempty"`
	Active    bool    `json:"active"`  // the chat's current head
	Message   Message `json:"message"` // the preview: ID, role and one text part
}

// Tree is a chat's tree and the revision it was read at.
type Tree struct {
	Revision int64       `json:"revision"`
	Entries  []TreeEntry `json:"entries"`
}

// TreeLabelRequest labels a message of the tree.
type TreeLabelRequest struct {
	ExpectedRevision int64  `json:"expected_revision"`
	Target           string `json:"target"`
	Label            string `json:"label"` // "" removes the label
}

// TreeMoveRequest switches or navigates a chat to another branch.
type TreeMoveRequest struct {
	ExpectedRevision int64  `json:"expected_revision"`
	Target           string `json:"target"`
	Summarize        bool   `json:"summarize,omitempty"` // navigate only
}

// TreeCopyRequest forks or clones a chat into a new one.
type TreeCopyRequest struct {
	ExpectedRevision int64  `json:"expected_revision"`
	Target           string `json:"target"`
	Fork             bool   `json:"fork"` // true = fork, false = clone
}

// TreeResult is a chat's revision after a tree action.
type TreeResult struct {
	Revision int64 `json:"revision"`
}

// TreeCopyResult describes the chat a fork or clone made.
type TreeCopyResult struct {
	SessionID string `json:"session_id"` // the new chat
	Prompt    string `json:"prompt"`     // fork: the prompt for the typing box
	Revision  int64  `json:"revision"`   // the source chat's revision afterwards
}

// TreeChanged is sent to every app after a chat's tree changed.
type TreeChanged struct {
	SessionID string `json:"session_id"`
	Revision  int64  `json:"revision"`
}

// FilePreviewRequest asks which files a move to Target would restore.
type FilePreviewRequest struct {
	Target string `json:"target"`
	Fork   bool   `json:"fork"`
}

// FilePlan is a file restore the engine keeps for a while under PlanID.
type FilePlan struct {
	PlanID    string          `json:"plan_id"` // the engine's handle for this plan
	SessionID string          `json:"session_id"`
	Target    string          `json:"target"`
	Source    string          `json:"source"`
	Revision  int64           `json:"revision"`
	UndoID    string          `json:"undo_id,omitempty"`
	Entries   []FilePlanEntry `json:"entries"`
}

// FilePlanEntry is one file of a plan, with the JSON names of
// filehistory.Entry.
type FilePlanEntry struct {
	Path        string          `json:"path"`
	Before      json.RawMessage `json:"before"` // filehistory.State, encoded as today
	After       json.RawMessage `json:"after"`
	Action      string          `json:"action"`
	Adds        int             `json:"adds"`
	Dels        int             `json:"dels"`
	Conflict    string          `json:"conflict,omitempty"`
	Unavailable string          `json:"unavailable,omitempty"`
	GitHead     string          `json:"git_head,omitempty"`
	GitMoved    bool            `json:"git_moved,omitempty"`
}

// FileUndoRequest applies a plan the engine keeps.
type FileUndoRequest struct {
	PlanID string `json:"plan_id"`
}

// FileRestoreResult is what applying a plan did.
type FileRestoreResult struct {
	Restored int      `json:"restored"`
	Skipped  []string `json:"skipped,omitempty"`
	UndoID   string   `json:"undo_id,omitempty"`
	Notice   string   `json:"notice"` // filehistory.Result.Notice()
}

// Branch copy (engine-design.md 5.11).

// BranchRequest asks for a branch from the highlighted words of a message.
type BranchRequest struct {
	MessageID      string `json:"message_id"`
	HighlightText  string `json:"highlight_text"`
	HighlightStart int    `json:"highlight_start"` // UTF-16 code units into the message text
	HighlightEnd   int    `json:"highlight_end"`
	Title          string `json:"title"` // already shortened by the app, as in the demo
}

// ChatLink links highlighted words in a message to the branch they opened.
type ChatLink struct {
	ID              string `json:"id"`
	SessionID       string `json:"session_id"`        // the branch the link opens
	ParentSessionID string `json:"parent_session_id"` // the chat that shows the link
	SourceMessageID string `json:"source_message_id"`
	HighlightText   string `json:"highlight_text"`
	HighlightStart  int    `json:"highlight_start"`
	HighlightEnd    int    `json:"highlight_end"`
	Origin          bool   `json:"origin"` // true: the branch's own parent link (used for nesting)
	CreatedAt       int64  `json:"created_at"`
}

// Branch describes the chat a branch made.
type Branch struct {
	Session       Session  `json:"session"` // the new chat
	Link          ChatLink `json:"link"`    // its origin link
	CopiedThrough string   `json:"copied_through"`
	Copied        int      `json:"copied"`
}

// TreeFromDomain builds the wire tree from the entries read at revision.
func TreeFromDomain(revision int64, entries []message.TreeEntry) Tree {
	out := Tree{Revision: revision, Entries: make([]TreeEntry, 0, len(entries))}
	for _, e := range entries {
		out.Entries = append(out.Entries, TreeEntry{
			MessageID: e.MessageID, ParentID: e.ParentID, Label: e.Label, Active: e.Active,
			Message: MessageFromDomain(e.Message),
		})
	}
	return out
}

// TreeToDomain is the inverse of TreeFromDomain, without the revision.
func TreeToDomain(t Tree) []message.TreeEntry {
	out := make([]message.TreeEntry, 0, len(t.Entries))
	for _, e := range t.Entries {
		out = append(out, message.TreeEntry{
			TreeNode: db.TreeNode{MessageID: e.MessageID, ParentID: e.ParentID, Label: e.Label, Active: e.Active},
			Message:  MessageToDomain(e.Message),
		})
	}
	return out
}

// FilePlanFromDomain builds the wire plan kept under planID. Its entries are
// encoded with filehistory.Entry's own JSON.
func FilePlanFromDomain(planID string, p *filehistory.Plan) (FilePlan, error) {
	out := FilePlan{
		PlanID: planID, SessionID: p.SessionID, Target: p.Target, Source: p.Source,
		Revision: p.Revision, UndoID: p.UndoID, Entries: []FilePlanEntry{},
	}
	if len(p.Entries) == 0 {
		return out, nil
	}
	data, err := json.Marshal(p.Entries)
	if err != nil {
		return FilePlan{}, err
	}
	if err := json.Unmarshal(data, &out.Entries); err != nil {
		return FilePlan{}, err
	}
	return out, nil
}

// FilePlanToDomain is the inverse of FilePlanFromDomain, without the plan ID.
func FilePlanToDomain(p FilePlan) (*filehistory.Plan, error) {
	out := &filehistory.Plan{
		SessionID: p.SessionID, Target: p.Target, Source: p.Source,
		Revision: p.Revision, UndoID: p.UndoID,
	}
	if len(p.Entries) == 0 {
		return out, nil
	}
	data, err := json.Marshal(p.Entries)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &out.Entries); err != nil {
		return nil, err
	}
	return out, nil
}

// FileRestoreResultFromDomain builds the wire result, with its notice.
func FileRestoreResultFromDomain(r filehistory.Result) FileRestoreResult {
	return FileRestoreResult{Restored: r.Restored, Skipped: r.Skipped, UndoID: r.UndoID, Notice: r.Notice()}
}

// FileRestoreResultToDomain is the inverse of FileRestoreResultFromDomain.
func FileRestoreResultToDomain(r FileRestoreResult) filehistory.Result {
	return filehistory.Result{Restored: r.Restored, Skipped: r.Skipped, UndoID: r.UndoID}
}

// ChatLinkFromDB builds the wire link.
func ChatLinkFromDB(l db.ChatLink) ChatLink {
	return ChatLink{
		ID: l.ID, SessionID: l.SessionID, ParentSessionID: l.ParentSessionID,
		SourceMessageID: l.SourceMessageID, HighlightText: l.HighlightText,
		HighlightStart: l.HighlightStart, HighlightEnd: l.HighlightEnd,
		Origin: l.CopiedFrom == "", CreatedAt: l.CreatedAt,
	}
}
