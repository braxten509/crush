package cliagent

import (
	"encoding/json"
	"strings"

	"github.com/charmbracelet/crush/internal/agent/tools"
)

// todoInput turns a CLI's own to-do or plan list into the input of Crush's
// todos tool, so every CLI fills the same to-do list. items is the list as
// the CLI sent it: Claude's TodoWrite, Grok's todo_write and OpenCode's
// todowrite send {content, status, activeForm}, Codex's plan sends
// {step, status} and ACP plans send {content, status}. Cancelled steps are
// left out, since Crush's list has no such state. ok is false when items is
// not a list of steps.
func todoInput(items any) (input string, ok bool) {
	list, isList := items.([]any)
	if !isList {
		return "", false
	}
	params := tools.TodosParams{Todos: []tools.TodoItem{}}
	for _, raw := range list {
		item, isMap := raw.(map[string]any)
		if !isMap {
			return "", false
		}
		text := func(keys ...string) string {
			for _, k := range keys {
				if s, ok := item[k].(string); ok && strings.TrimSpace(s) != "" {
					return strings.TrimSpace(s)
				}
			}
			return ""
		}
		content := text("content", "step", "text", "title", "subject", "description")
		status, known := todoStatus(text("status"))
		if content == "" || !known {
			return "", false
		}
		if status == "" {
			continue
		}
		params.Todos = append(params.Todos, tools.TodoItem{
			Content:    content,
			Status:     status,
			ActiveForm: text("activeForm", "active_form"),
		})
	}
	return marshal(params), true
}

// todoStatus maps a CLI's step status onto Crush's. A known status with an
// empty result is a cancelled step.
func todoStatus(s string) (string, bool) {
	switch strings.ToLower(strings.NewReplacer("-", "_", " ", "_").Replace(s)) {
	case "pending", "todo", "not_started", "":
		return "pending", true
	case "in_progress", "inprogress", "active", "running":
		return "in_progress", true
	case "completed", "complete", "done":
		return "completed", true
	case "cancelled", "canceled", "skipped":
		return "", true
	}
	return "", false
}

// todoListInput reads a to-do list from a tool call's input, which holds it
// under "todos" (or a plan's "plan" or "entries").
func todoListInput(raw json.RawMessage) (string, bool) {
	var in map[string]any
	if json.Unmarshal(raw, &in) != nil {
		return "", false
	}
	for _, k := range []string{"todos", "plan", "entries"} {
		if list, ok := in[k]; ok {
			return todoInput(list)
		}
	}
	return "", false
}
