package server

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/agent/tools/mcp"
	"github.com/charmbracelet/crush/internal/app"
	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/history"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/skills"
)

// wrapEvent converts a raw tea.Msg (a pubsub.Event[T] from the app
// event fan-in) into a pubsub.Payload envelope with the correct
// PayloadType discriminator and a proto-typed inner payload that has
// proper JSON tags. Returns nil if the event type is unrecognized.
func wrapEvent(ev any) *pubsub.Payload {
	switch e := ev.(type) {
	case pubsub.Event[app.LSPEvent]:
		return envelope(pubsub.PayloadTypeLSPEvent, pubsub.Event[proto.LSPEvent]{
			Type: e.Type,
			Payload: proto.LSPEvent{
				Type:            proto.LSPEventType(e.Payload.Type),
				Name:            e.Payload.Name,
				State:           e.Payload.State,
				Error:           e.Payload.Error,
				DiagnosticCount: e.Payload.DiagnosticCount,
			},
		})
	case pubsub.Event[mcp.Event]:
		payload, ok := proto.MCPEventFromDomain(e.Payload)
		if !ok {
			// Unsupported MCP event type. Drop it instead of fabricating
			// a state_changed event.
			slog.Debug("Dropping unsupported MCP event type for SSE", "type", e.Payload.Type)
			return nil
		}
		return envelope(pubsub.PayloadTypeMCPEvent, pubsub.Event[proto.MCPEvent]{Type: e.Type, Payload: payload})
	case pubsub.Event[permission.PermissionRequest]:
		return envelope(pubsub.PayloadTypePermissionRequest, pubsub.Event[proto.PermissionRequest]{
			Type:    e.Type,
			Payload: proto.PermissionRequestFromDomain(e.Payload),
		})
	case pubsub.Event[permission.PermissionNotification]:
		return envelope(pubsub.PayloadTypePermissionNotification, pubsub.Event[proto.PermissionNotification]{
			Type:    e.Type,
			Payload: proto.PermissionNotificationFromDomain(e.Payload),
		})
	case pubsub.Event[question.Request]:
		slog.Info("Wrapping question batch event for SSE", "id", e.Payload.ID, "questions", len(e.Payload.Questions))
		return envelope(pubsub.PayloadTypeQuestionRequest, pubsub.Event[proto.QuestionRequest]{
			Type:    e.Type,
			Payload: proto.QuestionRequestFromDomain(e.Payload),
		})
	case pubsub.Event[question.Notification]:
		return envelope(pubsub.PayloadTypeQuestionNotification, pubsub.Event[proto.QuestionNotification]{
			Type: e.Type,
			Payload: proto.QuestionNotification{
				BatchID:   e.Payload.BatchID,
				SessionID: e.Payload.SessionID,
			},
		})
	case pubsub.Event[message.Message]:
		return envelope(pubsub.PayloadTypeMessage, pubsub.Event[proto.Message]{
			Type:    e.Type,
			Payload: proto.MessageFromDomain(e.Payload),
		})
	case pubsub.Event[session.Session]:
		return envelope(pubsub.PayloadTypeSession, pubsub.Event[proto.Session]{
			Type:    e.Type,
			Payload: proto.SessionFromDomain(e.Payload),
		})
	case pubsub.Event[history.File]:
		return envelope(pubsub.PayloadTypeFile, pubsub.Event[proto.File]{
			Type:    e.Type,
			Payload: proto.FileFromDomain(e.Payload),
		})
	case pubsub.Event[notify.Notification]:
		return envelope(pubsub.PayloadTypeAgentEvent, pubsub.Event[proto.AgentEvent]{
			Type:    e.Type,
			Payload: proto.AgentEventFromDomain(e.Payload),
		})
	case pubsub.Event[notify.RunComplete]:
		return envelope(pubsub.PayloadTypeRunComplete, pubsub.Event[proto.RunComplete]{
			Type:    e.Type,
			Payload: proto.RunCompleteFromDomain(e.Payload),
		})
	case pubsub.Event[proto.ConfigChanged]:
		return envelope(pubsub.PayloadTypeConfigChanged, e)
	case app.UpdateAvailableMsg:
		return envelope(pubsub.PayloadTypeUpdateAvailable, pubsub.Event[proto.UpdateAvailable]{
			Type: pubsub.UpdatedEvent,
			Payload: proto.UpdateAvailable{
				CurrentVersion: e.CurrentVersion,
				LatestVersion:  e.LatestVersion,
				IsDevelopment:  e.IsDevelopment,
			},
		})
	case pubsub.Event[skills.Event]:
		return envelope(pubsub.PayloadTypeSkillsEvent, pubsub.Event[proto.SkillsEvent]{
			Type:    e.Type,
			Payload: proto.SkillsEventFromDomain(e.Payload),
		})
	case backend.EngineEvent:
		return envelope(e.Type, pubsub.Event[any]{Type: e.Kind, Payload: e.Payload})
	default:
		return wrapPieceEvent(ev)
	}
}

// sessionScopedEventID extracts the session ID from events that
// belong to a single session's interactive prompts (permission and
// question requests and their resolution notifications). The second
// return value reports whether the event is session-scoped at all. An
// event whose session ID is empty is treated as unscoped: it cannot
// match any client's current session, so scoping it would drop it for
// everyone.
func sessionScopedEventID(ev any) (string, bool) {
	var id string
	switch e := ev.(type) {
	case pubsub.Event[permission.PermissionRequest]:
		id = e.Payload.SessionID
	case pubsub.Event[permission.PermissionNotification]:
		id = e.Payload.SessionID
	case pubsub.Event[question.Request]:
		id = e.Payload.SessionID
	case pubsub.Event[question.Notification]:
		id = e.Payload.SessionID
	default:
		return "", false
	}
	return id, id != ""
}

// deliverToClient reports whether a workspace event should be written
// to the given client's SSE stream. Session-scoped prompt events are
// only delivered to clients currently viewing the session that raised
// them: a permission dialog or question form is only actionable there,
// and broadcasting it to every client in the workspace lets sessions
// steal each other's prompts. Prompts raised by sub-agent sessions
// (agent tool, agentic fetch) resolve to the top-level session so the
// viewer of the parent session can answer them. Clients on the landing
// screen (an explicitly cleared session) receive no prompts, while
// clients that never reported a session at all (e.g. API clients
// predating session reporting) fail open so their prompts are not
// stranded. All other events are delivered unfiltered.
func (c *controllerV1) deliverToClient(ctx context.Context, workspaceID, clientID string, ev any) bool {
	sessionID, scoped := sessionScopedEventID(ev)
	if !scoped {
		return true
	}
	current, reported := c.backend.ClientCurrentSession(workspaceID, clientID)
	if !reported {
		// The client never reported a current session, so there is
		// nothing to scope against. Fail open rather than stranding a
		// prompt nobody can answer.
		return true
	}
	if current == "" {
		return false
	}
	if sessionID == current {
		return true
	}
	root, err := c.backend.RootSessionID(ctx, workspaceID, sessionID)
	if err != nil {
		// Fail open: an unresolvable session is delivered as before
		// rather than stranding a prompt nobody can answer.
		slog.Debug("Failed to resolve root session for event routing",
			"session_id", sessionID, "error", err)
		return true
	}
	return root == current
}

// envelope marshals the inner event and wraps it in a pubsub.Payload.
func envelope(payloadType pubsub.PayloadType, inner any) *pubsub.Payload {
	raw, err := json.Marshal(inner)
	if err != nil {
		slog.Error("Failed to marshal event payload", "error", err)
		return nil
	}
	return &pubsub.Payload{
		Type:    payloadType,
		Payload: raw,
	}
}
