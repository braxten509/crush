package agent

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"

	crushlog "github.com/charmbracelet/crush/internal/log"
	"github.com/charmbracelet/crush/internal/message"
)

// recoverTurn turns a panic inside one agent turn into an error, so a bug
// in one turn ends that turn instead of the whole app. It saves the stack
// to a crash report and adds an error message to the session's chat.
// Deferred directly by runTurn.
func (c *coordinator) recoverTurn(ctx context.Context, sessionID string, errp *error) {
	r := recover()
	if r == nil {
		return
	}
	stack := debug.Stack()
	path := crushlog.SavePanicReport("agent-turn", r, stack)
	slog.Error("Recovered panic in agent turn", "session", sessionID, "panic", r, "report", path)

	details := fmt.Sprintf("%v", r)
	if path != "" {
		details += "\n\nDetails saved to " + path
	}
	*errp = fmt.Errorf("crush hit an internal error: %v", r)

	if c.messages == nil || sessionID == "" {
		return
	}
	msgCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, err := c.messages.Create(msgCtx, sessionID, message.CreateMessageParams{
		Role: message.Assistant,
		Parts: []message.ContentPart{message.Finish{
			Reason:  message.FinishReasonError,
			Time:    time.Now().Unix(),
			Message: "Crush hit an internal error",
			Details: details,
		}},
	})
	if err != nil {
		slog.Error("Failed to add the internal error to the chat", "error", err)
	}
}
