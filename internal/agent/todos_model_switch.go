package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
)

// handOverTodos clears the chat's to-do list when this turn's model is not
// the one that answered last: the list was that model's plan, and the new
// model keeps its own. It returns the old list as a note for the new model,
// so the goal isn't lost, or "" when nothing changed.
func (a *sessionAgent) handOverTodos(ctx context.Context, sessionID, provider, model string) string {
	current, err := a.sessions.Get(ctx, sessionID)
	if err != nil || len(current.Todos) == 0 {
		return ""
	}
	msgs, err := a.messages.List(ctx, sessionID)
	if err != nil {
		slog.Debug("Keeping the to-do list; the last reply's model is unknown", "error", err)
		return ""
	}
	if last, ok := lastModelReply(msgs); !ok || last.Provider == provider && last.Model == model {
		return ""
	}
	var old []session.Todo
	if _, err := a.sessions.Edit(ctx, sessionID, func(current *session.Session) bool {
		old, current.Todos = current.Todos, nil
		return len(old) > 0
	}); err != nil {
		slog.Error("Failed to clear the last model's to-do list", "error", err)
		return ""
	}
	if len(old) == 0 {
		return ""
	}
	return todoHandoverNote(old)
}

// lastModelReply is the newest reply a model wrote. Notices Crush adds itself
// (job results, skill changes) carry no model and are skipped.
func lastModelReply(msgs []message.Message) (message.Message, bool) {
	for i := len(msgs) - 1; i >= 0; i-- {
		if m := msgs[i]; m.Role == message.Assistant && !m.IsSummaryMessage && m.Provider != "" {
			return m, true
		}
	}
	return message.Message{}, false
}

// todoHandoverNote tells the new model what the last one planned.
func todoHandoverNote(todos []session.Todo) string {
	var b strings.Builder
	b.WriteString("<crush_previous_todo_list>\n")
	b.WriteString("The model that worked in this chat before you kept this to-do list. Crush cleared it from the screen because you are a different model. Use it to keep the goal: put the steps that still matter into your own to-do list, and leave out what is done or no longer fits.\n")
	for _, t := range todos {
		fmt.Fprintf(&b, "- [%s] %s\n", t.Status, t.Content)
	}
	b.WriteString("</crush_previous_todo_list>")
	return b.String()
}
