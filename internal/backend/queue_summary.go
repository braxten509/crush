package backend

import (
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/message"
)

func (b *Backend) QueuedPromptSummaries(id, sessionID string) ([]message.QueuedPromptSummary, error) {
	ws, err := b.GetWorkspace(id)
	if err != nil {
		return nil, err
	}
	return agent.QueueSummaries(ws.AgentCoordinator, sessionID), nil
}
