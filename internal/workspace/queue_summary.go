package workspace

import (
	"context"

	"github.com/charmbracelet/crush/internal/message"
)

func (w *AppWorkspace) AgentQueuedPromptSummaries(sessionID string) []message.QueuedPromptSummary {
	return w.ops.QueuedPromptSummaries(sessionID)
}

func (w *ClientWorkspace) AgentQueuedPromptSummaries(sessionID string) []message.QueuedPromptSummary {
	entries, err := w.client.GetAgentSessionQueuedPromptSummaries(context.Background(), w.workspaceID(), sessionID)
	if err != nil {
		return nil
	}
	return entries
}

// AgentQueuedPromptFiles lists the prompts AgentQueuedPromptsList lists,
// each with the files sent along. Only the in-process workspace has it: the
// phone's share, its one reader, runs there.
func (w *AppWorkspace) AgentQueuedPromptFiles(sessionID string) []message.QueuedPrompt {
	return w.ops.QueuedPromptFiles(sessionID)
}
