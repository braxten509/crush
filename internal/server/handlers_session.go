package server

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/rpc"
	"github.com/charmbracelet/crush/internal/session"
)

// sessionToWire converts a session for a REST response, filling the
// computed-on-read fields: busy and presence from the workspace (a nil
// workspace reports neither), unfinished helper or job work, and the
// answer the chat waits for.
func (c *controllerV1) sessionToWire(ctx context.Context, ws *backend.Workspace, s session.Session) proto.Session {
	out := proto.SessionFromDomain(s)
	out.IsBusy = isSessionBusy(ws, s.ID)
	out.AttachedClients = attachedClients(ws, s.ID)
	out.HasUnfinishedWork = agent.SessionHasUnfinishedWork(s.ID)
	if ws != nil {
		out.PendingPrompt = c.backend.PendingPrompt(ctx, ws.ID, s.ID)
	}
	return out
}

// isSessionBusy reports whether the given workspace has an in-flight
// agent run for sessionID. It tolerates a nil workspace (treating it as
// "not busy") so REST handlers can pass GetWorkspace's result through
// unconditionally — the workspace lookup error is already surfaced by
// the prior ListSessions/GetSession call when relevant.
func isSessionBusy(ws *backend.Workspace, sessionID string) bool {
	if ws == nil || ws.App == nil || ws.AgentCoordinator == nil {
		return false
	}
	return ws.AgentCoordinator.IsSessionBusy(sessionID)
}

// attachedClients returns the number of clients currently viewing
// sessionID in ws. Hold-only clients (streams == 0) do not contribute.
// A nil workspace is treated as zero so handlers can pass GetWorkspace's
// result through without an extra guard.
func attachedClients(ws *backend.Workspace, sessionID string) int {
	if ws == nil {
		return 0
	}
	return ws.AttachedClientsForSession(sessionID)
}

// sessionResult converts a single session result for a REST response.
func (c *controllerV1) sessionResult(r *http.Request, s session.Session, err error) (proto.Session, error) {
	if err != nil {
		return proto.Session{}, err
	}
	ws, _ := c.backend.GetWorkspace(r.PathValue("id"))
	return c.sessionToWire(r.Context(), ws, s), nil
}

// listSessions lists the sessions of a workspace.
func (c *controllerV1) listSessions(r *http.Request, _ rpc.Empty) ([]proto.Session, error) {
	id := r.PathValue("id")
	sessions, err := c.backend.ListSessions(r.Context(), id)
	if err != nil {
		return nil, err
	}
	ws, _ := c.backend.GetWorkspace(id)
	result := make([]proto.Session, len(sessions))
	for i, s := range sessions {
		result[i] = c.sessionToWire(r.Context(), ws, s)
	}
	return result, nil
}

// createSession creates a session titled after the request.
func (c *controllerV1) createSession(r *http.Request, args proto.Session) (proto.Session, error) {
	sess, err := c.backend.CreateSession(r.Context(), r.PathValue("id"), args.Title)
	return c.sessionResult(r, sess, err)
}

// getSession returns a single session.
func (c *controllerV1) getSession(r *http.Request, _ rpc.Empty) (proto.Session, error) {
	sess, err := c.backend.GetSession(r.Context(), r.PathValue("id"), r.PathValue("sid"))
	return c.sessionResult(r, sess, err)
}

// saveSession updates a session. The body is decoded as the wire type,
// not session.Session: the latter has no JSON tags, so snake_case fields
// such as prompt_tokens and summary_message_id would silently decode as
// zero and be persisted.
func (c *controllerV1) saveSession(r *http.Request, sess proto.Session) (proto.Session, error) {
	saved, err := c.backend.SaveSession(r.Context(), r.PathValue("id"), proto.SessionToDomain(sess))
	return c.sessionResult(r, saved, err)
}

// deleteSession deletes a session.
func (c *controllerV1) deleteSession(r *http.Request, _ rpc.Empty) (rpc.Empty, error) {
	return rpc.Empty{}, c.backend.DeleteSession(r.Context(), r.PathValue("id"), r.PathValue("sid"))
}

// setKeptSkill turns a skill on or off for a whole session.
func (c *controllerV1) setKeptSkill(r *http.Request, req proto.SetKeptSkillRequest) (proto.Session, error) {
	if req.ID == "" {
		return proto.Session{}, badRequest("id is required")
	}
	sess, err := c.backend.SetKeptSkill(r.Context(), r.PathValue("id"), r.PathValue("sid"),
		session.KeptSkill{ID: req.ID, Name: req.Name, ByAgent: req.Toggleable}, req.On)
	if err != nil {
		return proto.Session{}, err
	}
	return proto.SessionFromDomain(sess), nil
}

// setSessionModel saves the model one chat runs on.
func (c *controllerV1) setSessionModel(r *http.Request, req proto.SetSessionModelRequest) (proto.Session, error) {
	var choice struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	if json.Unmarshal(req.Model, &choice) != nil || choice.Provider == "" || choice.Model == "" {
		return proto.Session{}, badRequest("model is required")
	}
	sess, err := c.backend.SetSessionModel(r.Context(), r.PathValue("id"), r.PathValue("sid"),
		proto.SessionModelToDomain(req.Model))
	if err != nil {
		return proto.Session{}, err
	}
	return proto.SessionFromDomain(sess), nil
}

// getLastPick returns the model a new chat in this folder starts on.
func (c *controllerV1) getLastPick(r *http.Request, _ rpc.Empty) (config.SelectedModel, error) {
	return c.backend.LastPick(r.PathValue("id"))
}

// setLastPick saves a model picked with no chat open.
func (c *controllerV1) setLastPick(r *http.Request, req config.SelectedModel) (rpc.Empty, error) {
	if req.Provider == "" || req.Model == "" {
		return rpc.Empty{}, badRequest("model is required")
	}
	return rpc.Empty{}, c.backend.SetLastPick(r.PathValue("id"), req)
}

// listSavedSessions lists saved chats across every project.
func (c *controllerV1) listSavedSessions(r *http.Request, _ rpc.Empty) ([]session.Session, error) {
	// ?folders=N also lists the N most recently used projects' chats.
	folders, _ := strconv.Atoi(r.URL.Query().Get("folders"))
	return c.backend.ListRecentSavedSessions(r.Context(), r.PathValue("id"), r.URL.Query().Get("selected"), max(folders, 0))
}

// changeSavedSession renames or deletes a saved chat, or discards an
// empty one.
func (c *controllerV1) changeSavedSession(r *http.Request, change session.CatalogChange) (struct{}, error) {
	return struct{}{}, c.backend.ChangeSavedSession(r.Context(), r.PathValue("id"), change)
}

// getSavedSession reads a chat of another registered folder (?dir=) from
// that folder's database, so its engine need not run.
func (c *controllerV1) getSavedSession(r *http.Request, _ rpc.Empty) (proto.Session, error) {
	sess, _, err := c.backend.ReadSavedSession(r.Context(), r.PathValue("id"), r.URL.Query().Get("dir"), r.PathValue("sid"), false)
	if err != nil {
		return proto.Session{}, err
	}
	return proto.SessionFromDomain(sess), nil
}

// getSavedSessionMessages is getSessionMessages for a chat of another
// registered folder (?dir=), read from that folder's database.
func (c *controllerV1) getSavedSessionMessages(r *http.Request, _ rpc.Empty) ([]proto.Message, error) {
	_, messages, err := c.backend.ReadSavedSession(r.Context(), r.PathValue("id"), r.URL.Query().Get("dir"), r.PathValue("sid"), true)
	if err != nil {
		return nil, err
	}
	last, _ := strconv.Atoi(r.URL.Query().Get("last"))
	return proto.MessagesFromDomain(pageMessages(messages, last, r.URL.Query().Get("before"))), nil
}

// getSessionHistory returns the file versions recorded for a session.
func (c *controllerV1) getSessionHistory(r *http.Request, _ rpc.Empty) ([]proto.File, error) {
	history, err := c.backend.ListSessionHistory(r.Context(), r.PathValue("id"), r.PathValue("sid"))
	if err != nil {
		return nil, err
	}
	return proto.FilesFromDomain(history), nil
}

// getSessionMessages returns a session's messages: all of them, or with
// ?last=N the newest part, and with ?before=<message id> only those older
// than that message (a long chat opens with its newest part first and
// reads older pages as it scrolls up).
func (c *controllerV1) getSessionMessages(r *http.Request, _ rpc.Empty) ([]proto.Message, error) {
	messages, err := c.backend.ListSessionMessages(r.Context(), r.PathValue("id"), r.PathValue("sid"))
	if err != nil {
		return nil, err
	}
	last, _ := strconv.Atoi(r.URL.Query().Get("last"))
	return proto.MessagesFromDomain(pageMessages(messages, last, r.URL.Query().Get("before"))), nil
}

// pageMessages cuts msgs (oldest first) to those before the message with ID
// before (none when it is unknown), then to about the last N. A page starts
// at a user message when one is near, so a tool's result never shows
// without its call; it reaches back at most N more messages for one.
func pageMessages(msgs []message.Message, last int, before string) []message.Message {
	if before != "" {
		i := slices.IndexFunc(msgs, func(m message.Message) bool { return m.ID == before })
		if i < 0 {
			return nil
		}
		msgs = msgs[:i]
	}
	if last <= 0 || len(msgs) <= last {
		return msgs
	}
	start := len(msgs) - last
	for i := start; i >= 0 && i >= start-last; i-- {
		if msgs[i].Role == message.User {
			return msgs[i:]
		}
	}
	return msgs[start:]
}

// getMessageReview loads the retained file review of a message.
func (c *controllerV1) getMessageReview(r *http.Request, _ rpc.Empty) (proto.Message, error) {
	msg, err := c.backend.LoadMessageReview(r.Context(), r.PathValue("id"), r.PathValue("sid"), r.PathValue("mid"))
	if err != nil {
		return proto.Message{}, err
	}
	return proto.MessageFromDomain(msg), nil
}

// getSessionUserMessages returns the user messages of a session.
func (c *controllerV1) getSessionUserMessages(r *http.Request, _ rpc.Empty) ([]proto.Message, error) {
	messages, err := c.backend.ListUserMessages(r.Context(), r.PathValue("id"), r.PathValue("sid"))
	if err != nil {
		return nil, err
	}
	return proto.MessagesFromDomain(messages), nil
}

// getAllUserMessages returns the user messages of every session.
func (c *controllerV1) getAllUserMessages(r *http.Request, _ rpc.Empty) ([]proto.Message, error) {
	messages, err := c.backend.ListAllUserMessages(r.Context(), r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	return proto.MessagesFromDomain(messages), nil
}

// getFileTrackerFiles lists the files a session has read.
func (c *controllerV1) getFileTrackerFiles(r *http.Request, _ rpc.Empty) ([]string, error) {
	return c.backend.FileTrackerListReadFiles(r.Context(), r.PathValue("id"), r.PathValue("sid"))
}

// recordFileRead records that a session read a file.
func (c *controllerV1) recordFileRead(r *http.Request, req proto.FileTrackerReadRequest) (rpc.Empty, error) {
	return rpc.Empty{}, c.backend.FileTrackerRecordRead(r.Context(), r.PathValue("id"), req.SessionID, req.Path)
}

// getFileLastRead returns when a session last read a file.
func (c *controllerV1) getFileLastRead(r *http.Request, _ rpc.Empty) (time.Time, error) {
	q := r.URL.Query()
	return c.backend.FileTrackerLastReadTime(r.Context(), r.PathValue("id"), q.Get("session_id"), q.Get("path"))
}
