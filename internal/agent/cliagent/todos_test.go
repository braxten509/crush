package cliagent

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/charmbracelet/crush/internal/agent/tools"
)

func todoItems(t *testing.T, input string) []tools.TodoItem {
	t.Helper()
	var params tools.TodosParams
	require.NoError(t, json.Unmarshal([]byte(input), &params))
	return params.Todos
}

func TestClaudeTodoWriteBecomesCrushTodos(t *testing.T) {
	t.Parallel()
	name, input := claudeTool("TodoWrite", json.RawMessage(`{"todos":[{"content":"Fix it","status":"in_progress","activeForm":"Fixing it"},{"content":"Old idea","status":"cancelled"}]}`))
	require.Equal(t, tools.TodosToolName, name)
	require.Equal(t, []tools.TodoItem{{Content: "Fix it", Status: "in_progress", ActiveForm: "Fixing it"}}, todoItems(t, input), "cancelled steps are left out")
}

func TestToDoListShapes(t *testing.T) {
	t.Parallel()
	var codex any
	require.NoError(t, json.Unmarshal([]byte(`[{"step":"Read","status":"completed"},{"step":"Write","status":"inProgress"},{"step":"Test","status":"pending"}]`), &codex))
	input, ok := todoInput(codex)
	require.True(t, ok)
	require.Equal(t, []tools.TodoItem{
		{Content: "Read", Status: "completed"},
		{Content: "Write", Status: "in_progress"},
		{Content: "Test", Status: "pending"},
	}, todoItems(t, input))

	var unknown any
	require.NoError(t, json.Unmarshal([]byte(`[{"step":"Read","status":"blocked"}]`), &unknown))
	_, ok = todoInput(unknown)
	require.False(t, ok, "an unknown status is not guessed")
	_, ok = todoInput("not a list")
	require.False(t, ok)
}

func TestCodexPlanUpdateEmitsTodos(t *testing.T) {
	t.Parallel()
	var events []Event
	r := &codexRun{t: Turn{Emit: func(e Event) error { events = append(events, e); return nil }}}
	done, err := r.handleNotification(rpcMessage{Method: "turn/plan/updated", Params: json.RawMessage(`{"threadId":"th","turnId":"tu","plan":[{"step":"Write","status":"inProgress"}]}`)})
	require.NoError(t, err)
	require.False(t, done)
	require.Len(t, events, 1)
	require.Equal(t, EventTodos, events[0].Type)
	require.Equal(t, []tools.TodoItem{{Content: "Write", Status: "in_progress"}}, todoItems(t, events[0].Input))
}

func TestACPPlanAndToDoToolBecomeTodos(t *testing.T) {
	t.Parallel()
	var events []Event
	emit := func(e Event) error { events = append(events, e); return nil }
	var plan acpUpdate
	require.NoError(t, json.Unmarshal([]byte(`{"sessionUpdate":"plan","entries":[{"content":"Look","priority":"high","status":"completed"},{"content":"Fix","priority":"high","status":"in_progress"}]}`), &plan))
	require.NoError(t, acpHandleUpdate(plan, map[string]*acpTool{}, emit))
	require.Len(t, events, 1)
	require.Equal(t, EventTodos, events[0].Type)
	require.Equal(t, []tools.TodoItem{{Content: "Look", Status: "completed"}, {Content: "Fix", Status: "in_progress"}}, todoItems(t, events[0].Input))

	name, input := acpTool2Crush(&acpTool{title: "todowrite", kind: "other", input: map[string]any{"todos": []any{map[string]any{"content": "Fix", "status": "pending", "priority": "high", "id": "1"}}}})
	require.Equal(t, tools.TodosToolName, name)
	require.Equal(t, []tools.TodoItem{{Content: "Fix", Status: "pending"}}, todoItems(t, input))
}
