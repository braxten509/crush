package backend

import (
	"context"
	"path/filepath"
	"slices"
	"time"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/history"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/session"
)

// CreateSession creates a new session in the given workspace.
func (b *Backend) CreateSession(ctx context.Context, workspaceID, title string) (session.Session, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return session.Session{}, err
	}
	return o.CreateSession(ctx, title)
}

// GetSession retrieves a session by workspace and session ID.
func (b *Backend) GetSession(ctx context.Context, workspaceID, sessionID string) (session.Session, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return session.Session{}, err
	}
	return o.GetSession(ctx, sessionID)
}

// ListSessions returns all sessions in the given workspace.
func (b *Backend) ListSessions(ctx context.Context, workspaceID string) ([]session.Session, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return nil, err
	}
	return o.ListSessions(ctx)
}

// GetAgentSession returns session metadata with the agent's busy
// status, whether helpers or jobs still owe the chat a result, and what
// answer it waits for.
func (b *Backend) GetAgentSession(ctx context.Context, workspaceID, sessionID string) (proto.AgentSession, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return proto.AgentSession{}, err
	}
	se, err := o.GetSession(ctx, sessionID)
	if err != nil {
		return proto.AgentSession{}, err
	}
	return proto.AgentSession{
		Session: proto.Session{
			ID:                se.ID,
			Title:             se.Title,
			HasUnfinishedWork: agent.SessionHasUnfinishedWork(se.ID),
			PendingPrompt:     b.PendingPrompt(ctx, workspaceID, se.ID),
		},
		IsBusy: o.IsSessionBusy(sessionID),
	}, nil
}

// ListSessionMessages returns all messages for a session, draining
// debounced updates first so HTTP clients (and the TUI on session
// switch) observe the latest in-memory state.
func (b *Backend) ListSessionMessages(ctx context.Context, workspaceID, sessionID string) ([]message.Message, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return nil, err
	}
	return o.ListMessages(ctx, sessionID)
}

// ListSessionHistory returns the history items for a session.
func (b *Backend) ListSessionHistory(ctx context.Context, workspaceID, sessionID string) ([]history.File, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return nil, err
	}
	return o.ListSessionHistory(ctx, sessionID)
}

// SaveSession updates a session in the given workspace.
func (b *Backend) SaveSession(ctx context.Context, workspaceID string, sess session.Session) (session.Session, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return session.Session{}, err
	}
	return o.SaveSession(ctx, sess)
}

// DeleteSession deletes a session from the given workspace.
func (b *Backend) DeleteSession(ctx context.Context, workspaceID, sessionID string) error {
	o, err := b.ops(workspaceID)
	if err != nil {
		return err
	}
	return o.DeleteSession(ctx, sessionID)
}

// SetKeptSkill turns a skill on or off for a whole session.
func (b *Backend) SetKeptSkill(ctx context.Context, workspaceID, sessionID string, skill session.KeptSkill, on bool) (session.Session, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return session.Session{}, err
	}
	return o.SetKeptSkill(ctx, sessionID, skill, on)
}

// ListUserMessages returns user-role messages for a session.
func (b *Backend) ListUserMessages(ctx context.Context, workspaceID, sessionID string) ([]message.Message, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return nil, err
	}
	return o.ListUserMessages(ctx, sessionID)
}

// ListAllUserMessages returns all user-role messages across sessions.
func (b *Backend) ListAllUserMessages(ctx context.Context, workspaceID string) ([]message.Message, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return nil, err
	}
	return o.ListAllUserMessages(ctx)
}

// LoadMessageReview loads the retained file review of a message in a
// session.
func (b *Backend) LoadMessageReview(ctx context.Context, workspaceID, sessionID, messageID string) (message.Message, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return message.Message{}, err
	}
	return o.LoadMessageReview(ctx, sessionID, messageID)
}

// ListSavedSessions lists the workspace's saved chats; old empty drafts
// in its own database are discarded. An empty draft that an app shows on
// any of this engine's workspaces using that database is never discarded.
func (b *Backend) ListSavedSessions(ctx context.Context, workspaceID, selected string) ([]session.Session, error) {
	return b.ListRecentSavedSessions(ctx, workspaceID, selected, 0)
}

// ListRecentSavedSessions is ListSavedSessions plus the chats of the folders
// most recently used saved projects (the desktop's Recent).
func (b *Backend) ListRecentSavedSessions(ctx context.Context, workspaceID, selected string, folders int) ([]session.Session, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	o := ws.ops()
	return o.ListSavedSessionsShown(ctx, selected, b.shownInDataDir(o.savedSessions().DataDirectory), folders)
}

// shownInDataDir lists the chats apps show on every workspace whose data
// directory is dir.
func (b *Backend) shownInDataDir(dir string) []string {
	var shown []string
	for _, other := range b.workspaces.Seq2() {
		if other.Cfg == nil || other.Cfg.Config().Options == nil ||
			filepath.Clean(other.Cfg.Config().Options.DataDirectory) != filepath.Clean(dir) {
			continue
		}
		for _, id := range other.shownSessions() {
			if !slices.Contains(shown, id) {
				shown = append(shown, id)
			}
		}
	}
	return shown
}

// ChangeSavedSession renames, deletes, or (with DiscardEmpty) discards
// an empty saved chat.
func (b *Backend) ChangeSavedSession(ctx context.Context, workspaceID string, change session.CatalogChange) error {
	o, err := b.ops(workspaceID)
	if err != nil {
		return err
	}
	if change.DiscardEmpty {
		return o.DiscardEmptySession(ctx, change.Session.ID)
	}
	return o.ChangeSavedSession(ctx, change.Session, change.Remove)
}

// ReadSavedSession reads a chat of the registered folder dir from that
// folder's own database: its record and, with messages, its messages.
func (b *Backend) ReadSavedSession(ctx context.Context, workspaceID, dir, sessionID string, messages bool) (session.Session, []message.Message, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return session.Session{}, nil, err
	}
	return o.savedSessions().Read(ctx, dir, sessionID, messages)
}

// FileTrackerRecordRead records a file read for a session.
func (b *Backend) FileTrackerRecordRead(ctx context.Context, workspaceID, sessionID, path string) error {
	o, err := b.ops(workspaceID)
	if err != nil {
		return err
	}
	o.FileTrackerRecordRead(ctx, sessionID, path)
	return nil
}

// FileTrackerLastReadTime returns the last read time for a file in a
// session.
func (b *Backend) FileTrackerLastReadTime(ctx context.Context, workspaceID, sessionID, path string) (time.Time, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return time.Time{}, err
	}
	return o.FileTrackerLastReadTime(ctx, sessionID, path), nil
}

// FileTrackerListReadFiles returns the list of read files for a session.
func (b *Backend) FileTrackerListReadFiles(ctx context.Context, workspaceID, sessionID string) ([]string, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return nil, err
	}
	return o.FileTrackerListReadFiles(ctx, sessionID)
}
