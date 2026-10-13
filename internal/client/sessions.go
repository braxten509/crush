package client

import (
	"context"
	"net/url"
	"time"

	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/rpc"
	"github.com/charmbracelet/crush/internal/session"
)

// ListSessions lists all sessions in a workspace as proto types.
func (c *Client) ListSessions(ctx context.Context, id string) ([]proto.Session, error) {
	return newCall(c, rpc.ListSessions, "get sessions", "sessions").at(id).get(ctx)
}

// CreateSession creates a new session in a workspace as a proto type.
func (c *Client) CreateSession(ctx context.Context, id string, title string) (*proto.Session, error) {
	sess, err := newCall(c, rpc.CreateSession, "create session", "session").at(id).do(ctx, proto.Session{Title: title})
	if err != nil {
		return nil, err
	}
	return &sess, nil
}

// GetSession retrieves a specific session as a proto type.
func (c *Client) GetSession(ctx context.Context, id string, sessionID string) (*proto.Session, error) {
	sess, err := newCall(c, rpc.GetSession, "get session", "session").at(id, sessionID).get(ctx)
	if err != nil {
		return nil, err
	}
	return &sess, nil
}

// SaveSession updates a session in a workspace, returning a proto type.
func (c *Client) SaveSession(ctx context.Context, id string, sess proto.Session) (*proto.Session, error) {
	saved, err := newCall(c, rpc.SaveSession, "save session", "session").at(id, sess.ID).do(ctx, sess)
	if err != nil {
		return nil, err
	}
	return &saved, nil
}

// DeleteSession deletes a session from a workspace.
func (c *Client) DeleteSession(ctx context.Context, id string, sessionID string) error {
	_, err := newCall(c, rpc.DeleteSession, "delete session", "").at(id, sessionID).get(ctx)
	return err
}

// SetKeptSkill turns a skill on or off for a whole session.
func (c *Client) SetKeptSkill(ctx context.Context, id, sessionID string, skill proto.KeptSkill, on bool) (*proto.Session, error) {
	sess, err := newCall(c, rpc.SetKeptSkill, "set kept skill", "session").at(id, sessionID).
		do(ctx, proto.SetKeptSkillRequest{ID: skill.ID, Name: skill.Name, On: on, Toggleable: skill.ByAgent})
	if err != nil {
		return nil, err
	}
	return &sess, nil
}

// ListSavedSessions lists saved chats across every project.
func (c *Client) ListSavedSessions(ctx context.Context, workspace, selected string) ([]session.Session, error) {
	return newCall(c, rpc.ListSavedSessions, "list saved chats", "saved chats").
		at(workspace).with(url.Values{"selected": []string{selected}}).get(ctx)
}

// ChangeSavedSession renames or deletes a saved chat.
func (c *Client) ChangeSavedSession(ctx context.Context, workspace string, target session.Session, remove bool) error {
	_, err := newCall(c, rpc.ChangeSavedSession, "change saved chat", "saved chat response").at(workspace).
		do(ctx, session.CatalogChange{Session: target, Remove: remove})
	return err
}

// DiscardEmptySession deletes a chat that has no messages.
func (c *Client) DiscardEmptySession(ctx context.Context, workspace, id string) error {
	_, err := newCall(c, rpc.ChangeSavedSession, "discard empty chat", "saved chat response").at(workspace).
		do(ctx, session.CatalogChange{Session: session.Session{ID: id}, DiscardEmpty: true})
	return err
}

// ListMessages retrieves all messages for a session as proto types.
func (c *Client) ListMessages(ctx context.Context, id string, sessionID string) ([]proto.Message, error) {
	return newCall(c, rpc.SessionMessages, "get messages", "messages").at(id, sessionID).get(ctx)
}

// ListUserMessages retrieves user-role messages for a session as proto types.
func (c *Client) ListUserMessages(ctx context.Context, id string, sessionID string) ([]proto.Message, error) {
	return newCall(c, rpc.SessionUserMessages, "get user messages", "user messages").at(id, sessionID).get(ctx)
}

// ListAllUserMessages retrieves all user-role messages across sessions as proto types.
func (c *Client) ListAllUserMessages(ctx context.Context, id string) ([]proto.Message, error) {
	return newCall(c, rpc.AllUserMessages, "get all user messages", "all user messages").at(id).get(ctx)
}

// LoadMessageReview loads the retained file review of a message.
func (c *Client) LoadMessageReview(ctx context.Context, workspaceID, sessionID, messageID string) (proto.Message, error) {
	msg, err := newCall(c, rpc.MessageReview, "load review", "review").at(workspaceID, sessionID, messageID).get(ctx)
	if err != nil {
		return proto.Message{}, &reviewUnavailableError{err: err}
	}
	return msg, nil
}

// reviewUnavailableError keeps the user-facing explanation for a missing
// review while still exposing the underlying error (for example
// [ErrNotFound]) to errors.Is.
type reviewUnavailableError struct{ err error }

func (e *reviewUnavailableError) Error() string {
	return "diff details are unavailable (only the latest five prompts keep them)"
}

func (e *reviewUnavailableError) Unwrap() error { return e.err }

// ListSessionHistoryFiles retrieves history files for a session as proto types.
func (c *Client) ListSessionHistoryFiles(ctx context.Context, id string, sessionID string) ([]proto.File, error) {
	return newCall(c, rpc.SessionHistory, "get session history files", "session history files").at(id, sessionID).get(ctx)
}

// FileTrackerRecordRead records a file read for a session.
func (c *Client) FileTrackerRecordRead(ctx context.Context, id string, sessionID, path string) error {
	_, err := newCall(c, rpc.FileTrackerRead, "record file read", "").at(id).
		do(ctx, proto.FileTrackerReadRequest{SessionID: sessionID, Path: path})
	return err
}

// FileTrackerLastReadTime returns the last read time for a file in a
// session.
func (c *Client) FileTrackerLastReadTime(ctx context.Context, id string, sessionID, path string) (time.Time, error) {
	return newCall(c, rpc.FileTrackerLastRead, "get last read time", "last read time").at(id).
		with(url.Values{"session_id": []string{sessionID}, "path": []string{path}}).get(ctx)
}

// FileTrackerListReadFiles returns the list of read files for a session.
func (c *Client) FileTrackerListReadFiles(ctx context.Context, id string, sessionID string) ([]string, error) {
	return newCall(c, rpc.FileTrackerFiles, "get read files", "read files").at(id, sessionID).get(ctx)
}
