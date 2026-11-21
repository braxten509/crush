package agent

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/message"
)

func (a *sessionAgent) codexCredits() (message.CodexCreditStore, bool) {
	storage, ok := a.messages.(interface {
		CodexCredits() (message.CodexCreditStore, bool)
	})
	if !ok {
		return nil, false
	}
	return storage.CodexCredits()
}

func (a *sessionAgent) recordCodexCredits(sessionID string) func(cliagent.CodexUsage) {
	storage, ok := a.codexCredits()
	if !ok {
		return nil
	}
	return func(usage cliagent.CodexUsage) {
		// An interrupted turn can still have processed billable tokens.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := storage.RecordCodexCredits(ctx, sessionID, usage.ThreadID, usage.TurnID, usage.Credits); err != nil {
			slog.Warn("Could not save Codex credit usage", "session_id", sessionID, "error", err)
		}
	}
}

// saveCompletionCredits snapshots the chat total on the reply that triggers
// the completion alert, so the number on older replies stays as it was then.
func (a *sessionAgent) saveCompletionCredits(assistant *message.Message) {
	storage, ok := a.codexCredits()
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	credits, err := storage.CodexCreditsUsed(ctx, assistant.SessionID)
	if err != nil {
		slog.Warn("Could not read Codex credit usage", "session_id", assistant.SessionID, "error", err)
		return
	}
	if credits <= 0 {
		return
	}
	for i, part := range assistant.Parts {
		if finish, ok := part.(message.Finish); ok {
			finish.EstimatedCodexCredits = credits
			finish.CodexCreditSessionID = assistant.SessionID
			reply := *assistant
			reply.Parts = slices.Clone(assistant.Parts)
			reply.Parts[i] = finish
			if err := a.messages.Update(ctx, reply); err != nil {
				slog.Warn("Could not save Codex credits on reply", "message_id", assistant.ID, "error", err)
			}
			return
		}
	}
}
