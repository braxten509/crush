package agent

import (
	"context"
	"sync"

	"github.com/charmbracelet/crush/internal/memwatch"
	"github.com/charmbracelet/crush/internal/message"
)

// memoryStatus reads the computer's memory for a turn starting now. Tests
// replace it so recorded prompts don't depend on the machine's memory.
var memoryStatus = func() memwatch.Status { return memwatch.Default.Recent(memwatch.CheckEvery) }

// memoryNoticed maps a session ID to the last memory episode its chat was
// told about.
var memoryNoticed sync.Map

// memoryNotice shows a one-line notice in the chat when a turn starts while
// memory is high, so the user sees why the agent holds off on big work. It
// shows once per chat each time memory gets high.
func (a *sessionAgent) memoryNotice(ctx context.Context, sessionID string) {
	showMemoryNotice(ctx, a.messages, sessionID, memoryStatus())
}

func showMemoryNotice(ctx context.Context, messages message.Service, sessionID string, s memwatch.Status) {
	if !s.High || messages == nil {
		return
	}
	if prev, ok := memoryNoticed.Swap(sessionID, s.Episode); ok && prev.(int) == s.Episode {
		return
	}
	// The system role never becomes part of the model's context.
	_, _ = messages.Create(ctx, sessionID, message.CreateMessageParams{
		Role:  message.System,
		Parts: []message.ContentPart{message.TextContent{Text: s.ChatNotice()}},
	})
}
