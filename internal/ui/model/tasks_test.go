package model

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestTaskRowOpensMatchingDialog(t *testing.T) {
	t.Parallel()
	m := newTestUI()
	m.dialog = dialog.NewOverlay()
	m.session = &session.Session{ID: "s1"}
	m.tasks = map[string]agent.Task{
		"t1": {ID: "t1", SessionID: "s1", Name: "Review", Status: agent.TaskRunning, Started: time.Now()},
		"t2": {ID: "t2", SessionID: "s1", Name: "Benchmark speech", Status: agent.TaskRunning, Started: time.Now()},
	}
	m.bgProcs = []agent.Process{{PID: 42, Command: "sleep 900", Started: time.Now()}}
	require.Equal(t, 2, m.taskRowLen(), "one entry per category, regardless of agent count")
	m.tasksFocused = true
	m.taskSel = 0
	_, handled := m.handleTaskRowKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.True(t, handled)
	require.True(t, m.dialog.ContainsDialog(dialog.SubAgentsID))
	require.False(t, m.dialog.ContainsDialog(dialog.BackgroundID))
	scr := uv.NewScreenBuffer(100, 24)
	m.dialog.Dialog(dialog.SubAgentsID).Draw(scr, scr.Bounds())
	out := ansi.Strip(scr.Render())
	t.Log("\n" + out)
	require.Contains(t, out, "Review")
	require.Contains(t, out, "Benchmark speech")

	m.tasksFocused = true
	m.handleTaskRowKey(tea.KeyPressMsg{Code: tea.KeyRight})
	require.Equal(t, 1, m.taskSel)
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

func TestTaskRowCollapsesAgentDetails(t *testing.T) {
	t.Parallel()
	m := newTestUI()
	m.session = &session.Session{ID: "s1"}
	m.tasks = map[string]agent.Task{
		"t1": {ID: "t1", SessionID: "s1", Name: "Benchmark offline speech-to-text on Pixel", CLI: "claude", Status: agent.TaskRunning},
		"t2": {ID: "t2", SessionID: "s1", Name: "Evaluate Hey Jarvis wake word", CLI: "codex", Status: agent.TaskRunning},
	}
	m.bgProcs = []agent.Process{{PID: 42}}
	for _, width := range []int{60, 120} {
		for _, focused := range []bool{false, true} {
			m.tasksFocused = focused
			row := m.renderTasks(width)
			out := ansi.Strip(row)
			require.Contains(t, out, "2 subagents")
			require.Contains(t, out, "1 background process")
			for _, task := range m.tasks {
				require.NotContains(t, out, task.Name)
				require.NotContains(t, out, task.CLI)
			}
			require.NotContains(t, out, "x stop")
			require.NotContains(t, row, "\n")
			require.LessOrEqual(t, ansi.StringWidth(row), width)
			t.Logf("width=%d focused=%v: %s", width, focused, row)
		}
	}
}

// Ctrl+X with the sub-agents entry selected opens the sub-agents list, not
// the background processes one.
func TestTaskRowBackgroundKeyOpensSelectedEntry(t *testing.T) {
	t.Parallel()
	m := newFrameTestUI(t)
	m.state = uiChat
	m.focus = uiFocusEditor
	m.session = &session.Session{ID: "s1"}
	m.tasks = map[string]agent.Task{
		"t1": {ID: "t1", SessionID: "s1", Name: "Review", Status: agent.TaskRunning, Started: time.Now()},
	}
	m.tasksFocused = true
	m.taskSel = 0

	m.Update(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	require.True(t, m.dialog.ContainsDialog(dialog.SubAgentsID))
	require.False(t, m.dialog.ContainsDialog(dialog.BackgroundID))
}

// heightCountingEditor counts how often the layout measures it.
type heightCountingEditor struct {
	dialog.InlineEditor
	calls int
}

func (e *heightCountingEditor) Height(width int) int {
	e.calls++
	return e.InlineEditor.Height(width)
}

// While a question form hides the task row, background process polls must
// not relayout the UI every time.
func TestTaskRowRefreshSkipsRelayoutUnderInlineEditor(t *testing.T) {
	t.Parallel()
	m := newFrameTestUI(t)
	m.state = uiChat
	m.bgProcs = []agent.Process{{PID: 42, Command: "sleep 900", Started: time.Now()}}
	m.openBatchFormDialog(question.Request{
		ID:        "q",
		Questions: []question.Question{{ID: "q1", Type: question.TypeYesNo, Text: "Proceed?"}},
	})
	editor := &heightCountingEditor{InlineEditor: m.activeInline}
	m.activeInline = editor

	m.relayoutTasks()
	require.Zero(t, editor.calls)
	require.Zero(t, m.layout.tasks.Dy())
}
