package server

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/agent/tools/mcp"
	"github.com/charmbracelet/crush/internal/app"
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
			Type:    e.Type,
			Payload: proto.QuestionNotification{BatchID: e.Payload.BatchID},
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
	default:
		slog.Warn("Unrecognized event type for SSE wrapping", "type", fmt.Sprintf("%T", ev))
		return nil
	}
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
