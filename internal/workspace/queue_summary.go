package workspace

import (
	"context"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/message"
)

func (w *AppWorkspace) AgentQueuedPromptSummaries(sessionID string) []message.QueuedPromptSummary {
	return agent.QueueSummaries(w.app.AgentCoordinator, sessionID)
}
func (w *ClientWorkspace) AgentQueuedPromptSummaries(sessionID string) []message.QueuedPromptSummary {
	entries, err := w.client.GetAgentSessionQueuedPromptSummaries(context.Background(), w.workspaceID(), sessionID)
	if err != nil {
		return nil
	}
	return entries
}
