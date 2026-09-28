package model

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/stretchr/testify/require"
)

func TestTaskRowOpensMatchingDialog(t *testing.T) {
	t.Parallel()
	m := newTestUI()
	m.dialog = dialog.NewOverlay()
	m.session = &session.Session{ID: "s1"}
	m.tasks = map[string]agent.Task{
		"t1": {ID: "t1", SessionID: "s1", Name: "Review", Status: agent.TaskRunning, Started: time.Now()},
	}
	m.bgProcs = []agent.Process{{PID: 42, Command: "sleep 900", Started: time.Now()}}
	m.tasksFocused = true
	m.taskSel = 0
	_, handled := m.handleTaskRowKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.True(t, handled)
	require.True(t, m.dialog.ContainsDialog(dialog.SubAgentsID))
	require.False(t, m.dialog.ContainsDialog(dialog.BackgroundID))

	m.tasksFocused = true
	m.taskSel = 1
	_, handled = m.handleTaskRowKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.True(t, handled)
	require.True(t, m.dialog.ContainsDialog(dialog.BackgroundID))
	require.False(t, m.dialog.ContainsDialog(dialog.SubAgentsID))

	// Explicit openers also work when neither category has running items.
	m.tasks = nil
	m.bgProcs = nil
	m.openSubAgentsDialog()
	require.True(t, m.dialog.ContainsDialog(dialog.SubAgentsID))
	m.openBackgroundDialog()
	require.True(t, m.dialog.ContainsDialog(dialog.BackgroundID))
	require.False(t, m.dialog.ContainsDialog(dialog.SubAgentsID))
}
