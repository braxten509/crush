package tools

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/charmbracelet/crush/internal/session"
)

func TestUpdateTodosReportsWhatChanged(t *testing.T) {
	t.Parallel()
	old := []session.Todo{{Content: "Read", Status: session.TodoStatusInProgress}, {Content: "Write", Status: session.TodoStatusPending}}
	todos, meta, err := UpdateTodos(old, []TodoItem{
		{Content: "Read", Status: "completed"},
		{Content: "Write", Status: "in_progress", ActiveForm: "Writing"},
	})
	require.NoError(t, err)
	require.Len(t, todos, 2)
	require.False(t, meta.IsNew)
	require.Equal(t, []string{"Read"}, meta.JustCompleted)
	require.Equal(t, "Writing", meta.JustStarted)
	require.Equal(t, 1, meta.Completed)
	require.Equal(t, 2, meta.Total)

	_, _, err = UpdateTodos(nil, []TodoItem{{Content: "Read", Status: "blocked"}})
	require.Error(t, err)
}
