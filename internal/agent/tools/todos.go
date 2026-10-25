package tools

import (
	"context"
	_ "embed"
	"fmt"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/session"
)

//go:embed todos.md
var todosDescription string

const TodosToolName = "todos"

type TodosParams struct {
	Todos []TodoItem `json:"todos" description:"The updated todo list"`
}

type TodoItem struct {
	Content    string `json:"content" description:"What needs to be done (imperative form)"`
	Status     string `json:"status" description:"Task status: pending, in_progress, or completed"`
	ActiveForm string `json:"active_form" description:"Present continuous form (e.g., 'Running tests')"`
}

type TodosResponseMetadata struct {
	IsNew         bool           `json:"is_new"`
	Todos         []session.Todo `json:"todos"`
	JustCompleted []string       `json:"just_completed,omitempty"`
	JustStarted   string         `json:"just_started,omitempty"`
	Completed     int            `json:"completed"`
	Total         int            `json:"total"`
}

func NewTodosTool(sessions session.Service) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		TodosToolName,
		todosDescription,
		func(ctx context.Context, params TodosParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			sessionID := GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, fmt.Errorf("session ID is required for managing todos")
			}

			var todos []session.Todo
			var metadata TodosResponseMetadata
			var invalid error
			_, err := sessions.Edit(ctx, sessionID, func(current *session.Session) bool {
				todos, metadata, invalid = UpdateTodos(current.Todos, params.Todos)
				if invalid != nil {
					return false
				}
				current.Todos = todos
				return true
			})
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("failed to save todos: %w", err)
			}
			if invalid != nil {
				return fantasy.ToolResponse{}, invalid
			}

			response := "Todo list updated successfully.\n\n"

			pendingCount := 0
			inProgressCount := 0

			for _, todo := range todos {
				switch todo.Status {
				case session.TodoStatusPending:
					pendingCount++
				case session.TodoStatusInProgress:
					inProgressCount++
				}
			}

			response += fmt.Sprintf("Status: %d pending, %d in progress, %d completed\n",
				pendingCount, inProgressCount, metadata.Completed)

			response += "Todos have been modified successfully. Ensure that you continue to use the todo list to track your progress. Please proceed with the current tasks if applicable."

			return fantasy.WithResponseMetadata(fantasy.NewTextResponse(response), metadata), nil
		},
	)
}

// UpdateTodos checks a new to-do list against the old one and returns it with
// what changed, for the todos tool and for agent CLIs' own to-do lists.
func UpdateTodos(old []session.Todo, items []TodoItem) ([]session.Todo, TodosResponseMetadata, error) {
	for _, item := range items {
		switch item.Status {
		case "pending", "in_progress", "completed":
		default:
			return nil, TodosResponseMetadata{}, fmt.Errorf("invalid status %q for todo %q", item.Status, item.Content)
		}
	}
	oldStatusByContent := make(map[string]session.TodoStatus)
	for _, todo := range old {
		oldStatusByContent[todo.Content] = todo.Status
	}
	todos := make([]session.Todo, len(items))
	meta := TodosResponseMetadata{IsNew: len(old) == 0, Total: len(items)}
	for i, item := range items {
		todos[i] = session.Todo{
			Content:    item.Content,
			Status:     session.TodoStatus(item.Status),
			ActiveForm: item.ActiveForm,
		}
		newStatus := session.TodoStatus(item.Status)
		oldStatus, existed := oldStatusByContent[item.Content]
		if newStatus == session.TodoStatusCompleted {
			meta.Completed++
			if existed && oldStatus != session.TodoStatusCompleted {
				meta.JustCompleted = append(meta.JustCompleted, item.Content)
			}
		}
		if newStatus == session.TodoStatusInProgress && (!existed || oldStatus != session.TodoStatusInProgress) {
			if item.ActiveForm != "" {
				meta.JustStarted = item.ActiveForm
			} else {
				meta.JustStarted = item.Content
			}
		}
	}
	meta.Todos = todos
	return todos, meta, nil
}
