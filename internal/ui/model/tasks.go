package model

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// A row under the editor has one entry for sub-agents and one for background
// processes. Down selects the row; Enter opens the selected category's list.

const (
	taskLinger     = 8 * time.Second
	bgProcsRefresh = time.Second
)

type (
	// taskTickMsg turns the spinner and expires completed sub-agents after their linger period.
	taskTickMsg struct{}
	// bgProcsMsg carries a fresh list of background processes.
	bgProcsMsg struct{ procs []agent.Process }
)

// taskSpinner is the half circle that turns while sub-agents run.
var taskSpinner = []string{"◐", "◓", "◑", "◒"}

func (m *UI) visibleTasks() []agent.Task {
	if !m.hasSession() {
		return nil
	}
	var out []agent.Task
	for _, t := range m.tasks {
		if t.SessionID == m.session.ID && t.Status == agent.TaskRunning {
			out = append(out, t)
		}
	}
	slices.SortFunc(out, func(a, b agent.Task) int { return a.Started.Compare(b.Started) })
	return out
}

// taskRowLen is how many categories the row has: sub-agents, then
// processes, if any.
func (m *UI) taskRowLen() int {
	n := 0
	if len(m.visibleTasks()) > 0 {
		n++
	}
	if len(m.bgProcs) > 0 {
		n++
	}
	return n
}

// tasksHeight is the row's height in the layout. It only shows in the chat,
// and an inline editor such as a question form takes its place.
func (m *UI) tasksHeight() int {
	if m.state == uiChat && m.activeInline == nil && m.taskRowLen() > 0 {
		return 1
	}
	return 0
}

// relayoutTasks keeps the layout and the selection in step with the row.
func (m *UI) relayoutTasks() {
	n := m.taskRowLen()
	if n == 0 {
		m.tasksFocused = false
	}
	m.taskSel = min(m.taskSel, max(n-1, 0))
	if m.tasksHeight() != m.layout.tasks.Dy() {
		m.updateLayoutAndSize()
	}
}

func (m *UI) handleTaskEvent(t agent.Task) tea.Cmd {
	if m.tasks == nil {
		m.tasks = map[string]agent.Task{}
	}
	m.tasks[t.ID] = t
	m.relayoutTasks()
	m.refreshBackgroundDialog()
	return m.taskTick()
}

// taskTick schedules the next refresh while any sub-agent is listed.
func (m *UI) taskTick() tea.Cmd {
	if m.taskTicking || len(m.visibleTasks()) == 0 {
		return nil
	}
	m.taskTicking = true
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return taskTickMsg{} })
}

func (m *UI) handleTaskTick() tea.Cmd {
	m.taskTicking = false
	for id, t := range m.tasks {
		if t.Status != agent.TaskRunning && time.Since(t.Ended) >= taskLinger {
			delete(m.tasks, id)
		}
	}
	m.relayoutTasks()
	return m.taskTick()
}

// pollBgProcs lists the background processes off the update loop.
func (m *UI) pollBgProcs() tea.Cmd {
	return tea.Tick(bgProcsRefresh, func(time.Time) tea.Msg {
		return bgProcsMsg{procs: agent.BackgroundProcesses()}
	})
}

func (m *UI) handleBgProcs(procs []agent.Process) tea.Cmd {
	m.bgProcs = procs
	m.relayoutTasks()
	m.refreshBackgroundDialog()
	return m.pollBgProcs()
}

// refreshBackgroundDialog keeps an open background dialog in step with
// what is actually running.
func (m *UI) refreshBackgroundDialog() {
	for _, id := range []string{dialog.BackgroundID, dialog.SubAgentsID} {
		if d, ok := m.dialog.Dialog(id).(*dialog.Background); ok {
			d.SetItems(m.visibleTasks(), m.bgProcs, "")
		}
	}
}

// focusTaskRow selects the row from the editor, if it has anything.
func (m *UI) focusTaskRow() bool {
	if m.taskRowLen() == 0 {
		return false
	}
	m.tasksFocused = true
	m.taskSel = min(m.taskSel, m.taskRowLen()-1)
	return true
}

// handleTaskRowKey handles keys while the row is selected. Anything it
// doesn't use gives focus back to the editor and is handled there.
func (m *UI) handleTaskRowKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	n := m.taskRowLen()
	if n == 0 {
		m.tasksFocused = false
		return nil, false
	}
	// The background key opens what's selected, like Enter.
	if key.Matches(msg, m.keyMap.Chat.Background) {
		return m.openTaskRowDialog(), true
	}
	switch msg.String() {
	case "left":
		m.taskSel = (m.taskSel + n - 1) % n
	case "right", "tab":
		m.taskSel = (m.taskSel + 1) % n
	case "up", "esc":
		m.tasksFocused = false
	case "enter", "space", "x", "delete", "backspace":
		return m.openTaskRowDialog(), true
	default:
		m.tasksFocused = false
		return nil, false
	}
	return nil, true
}

func (m *UI) renderTasks(width int) string {
	tasks := m.visibleTasks()
	t := m.com.Styles
	items := make([]string, 0, 2)
	if len(tasks) > 0 {
		label := fmt.Sprintf("%d subagents", len(tasks))
		if len(tasks) == 1 {
			label = "1 subagent"
		}
		// Spins while any sub-agent runs; taskTick redraws it every second.
		icon := t.Tool.IconSuccess.Render()
		for _, task := range tasks {
			if task.Status == agent.TaskRunning {
				icon = t.Pills.TodoSpinner.Render(taskSpinner[time.Now().Unix()%int64(len(taskSpinner))])
				break
			}
		}
		items = append(items, icon+" "+t.Pills.TodoLabel.Render(label))
	}
	if n := len(m.bgProcs); n > 0 {
		label := fmt.Sprintf("%d background processes", n)
		if n == 1 {
			label = "1 background process"
		}
		items = append(items, t.Pills.TodoSpinner.Render("⚙")+" "+t.Pills.TodoLabel.Render(label))
	}
	if len(items) == 0 {
		return ""
	}
	for i, item := range items {
		if m.tasksFocused && i == m.taskSel {
			items[i] = t.Dialog.SelectedItem.Render(ansi.Strip(item))
		} else {
			items[i] = " " + item + " "
		}
	}

	var hint string
	switch {
	case !m.tasksFocused:
		hint = "↓ manage"
	default:
		hint = "enter list · ←/→ select · ↑ back"
	}
	line := " " + strings.Join(items, " ") + "  " + t.Pills.HelpText.Render(hint)
	return ansi.Truncate(line, width, "…")
}

// splitTasks takes the task row off the bottom of the editor block.
func splitTasks(editor uv.Rectangle, rows int) (uv.Rectangle, uv.Rectangle) {
	tasks := editor
	tasks.Min.Y = max(editor.Max.Y-rows, editor.Min.Y)
	editor.Max.Y = tasks.Min.Y
	return editor, tasks
}

// openTaskRowDialog opens the dialog matching the selected row item.
func (m *UI) openTaskRowDialog() tea.Cmd {
	if m.taskSel == 0 && len(m.visibleTasks()) > 0 {
		return m.openSubAgentsDialog()
	}
	return m.openBackgroundDialog()
}

// openBackgroundDialog lists only background processes.
func (m *UI) openBackgroundDialog() tea.Cmd {
	m.tasksFocused = false
	m.dialog.CloseDialog(dialog.SubAgentsID)
	m.dialog.CloseDialog(dialog.BackgroundID)
	m.dialog.OpenDialog(dialog.NewBackground(m.com, m.bgProcs))
	return nil
}

// openSubAgentsDialog lists only sub-agents.
func (m *UI) openSubAgentsDialog() tea.Cmd {
	m.tasksFocused = false
	tasks := m.visibleTasks()
	m.dialog.CloseDialog(dialog.BackgroundID)
	m.dialog.CloseDialog(dialog.SubAgentsID)
	m.dialog.OpenDialog(dialog.NewSubAgents(m.com, tasks, ""))
	return nil
}
