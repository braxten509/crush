package backend

import (
	"context"
	"path/filepath"
	"slices"
	"time"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/history"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/openchats"
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
			Branch:            se.Branch,
			WorkDir:           se.WorkDir,
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
	// Ops.ListMessages settles calls a dead process left waiting, so
	// every remote client sees them answered.
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

// SetSessionModel saves the model one chat runs on.
func (b *Backend) SetSessionModel(ctx context.Context, workspaceID, sessionID string, model session.ModelChoice) (session.Session, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return session.Session{}, err
	}
	return o.SetSessionModel(ctx, sessionID, model)
}

// GitBranches returns the current Git branch and all local branch names of
// the workspace's working directory.
func (b *Backend) GitBranches(workspaceID string) (proto.GitBranchesResponse, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return proto.GitBranchesResponse{}, err
	}
	return o.GitBranches(), nil
}

// SetSessionBranch switches a single chat to branch (creating it when
// create is true) inside its own folder copy when branch differs from the
// main project folder's branch.
func (b *Backend) SetSessionBranch(ctx context.Context, workspaceID, sessionID, branch string, create bool) (session.Session, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return session.Session{}, err
	}
	return o.SetSessionBranch(ctx, sessionID, branch, create)
}

// CloseSessionBranch cleans up a closed chat's extra folder copy if clean.
func (b *Backend) CloseSessionBranch(ctx context.Context, workspaceID, sessionID string) (session.Session, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return session.Session{}, err
	}
	return o.CloseSessionBranch(ctx, sessionID)
}

// LastPick is the model a new chat in the workspace's folder starts on.
func (b *Backend) LastPick(workspaceID string) (config.SelectedModel, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return config.SelectedModel{}, err
	}
	pick, _ := o.LastPick()
	return pick, nil
}

// SetLastPick saves a pick made with no chat open.
func (b *Backend) SetLastPick(workspaceID string, model config.SelectedModel) error {
	o, err := b.ops(workspaceID)
	if err != nil {
		return err
	}
	return o.SetLastPick(model)
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
	return b.ListRecentSavedSessions(ctx, workspaceID, selected, 0, "")
}

// ListRecentSavedSessions is ListSavedSessions plus the chats of the folders
// most recently used saved projects (the desktop's Recent); with folder
// set, only that folder's chats.
func (b *Backend) ListRecentSavedSessions(ctx context.Context, workspaceID, selected string, folders int, folder string) ([]session.Session, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	o := ws.ops()
	return o.ListSavedSessionsShown(ctx, selected, b.shownInDataDir(o.savedSessions().DataDirectory), folders, folder)
}

// shownInDataDir lists the chats apps show on every workspace whose data
// directory is dir, plus every chat in the open-chats list the terminal and
// desktop share: a listed chat stays until it is closed, even while another
// chat or folder is shown (an empty "New Session" in a second folder was
// discarded under the desktop, and its next save failed on the missing chat).
func (b *Backend) shownInDataDir(dir string) []string {
	var shown []string
	if list, err := openchats.Open(); err == nil {
		for _, c := range list.Chats() {
			if c.ID != "" && !slices.Contains(shown, c.ID) {
				shown = append(shown, c.ID)
			}
		}
	}
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
