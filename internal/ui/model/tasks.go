package model

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/util"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// A row under the editor lists background sub-agents (while they run, and
// briefly after they end) and how many processes the agents left running.
// Down from the end of the editor selects it; left/right pick an item.

const (
	taskLinger     = 8 * time.Second
	bgProcsRefresh = 2 * time.Second
)

type (
	// taskTickMsg refreshes the sub-agents' elapsed times.
	taskTickMsg struct{}
	// bgProcsMsg carries a fresh list of background processes.
	bgProcsMsg struct{ procs []agent.Process }
)

var taskSpinner = []string{"◐", "◓", "◑", "◒"}

func (m *UI) visibleTasks() []agent.Task {
	if !m.hasSession() {
		return nil
	}
	var out []agent.Task
	for _, t := range m.tasks {
		if t.SessionID == m.session.ID && (t.Status == agent.TaskRunning || time.Since(t.Ended) < taskLinger) {
			out = append(out, t)
		}
	}
	slices.SortFunc(out, func(a, b agent.Task) int { return a.Started.Compare(b.Started) })
	return out
}

// taskRowLen is how many items the row has: the sub-agents, then the
// processes, if any.
func (m *UI) taskRowLen() int {
	n := len(m.visibleTasks())
	if len(m.bgProcs) > 0 {
		n++
	}
	return n
}

func (m *UI) tasksHeight() int {
	if m.taskRowLen() > 0 {
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
	tasks := m.visibleTasks()
	n := m.taskRowLen()
	switch msg.String() {
	case "left":
		m.taskSel = (m.taskSel + n - 1) % n
	case "right", "tab":
		m.taskSel = (m.taskSel + 1) % n
	case "up", "esc":
		m.tasksFocused = false
	case "x", "delete", "backspace":
		if m.taskSel < len(tasks) {
			t := tasks[m.taskSel]
			if t.Status != agent.TaskRunning {
				return nil, true
			}
			return func() tea.Msg {
				if err := agent.StopTask(t.ID); err != nil {
					return util.ReportError(err)()
				}
				return util.NewInfoMsg("Stopped sub-agent " + t.Name)
			}, true
		}
		return m.openBackgroundDialog(), true
	case "enter", "space":
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
	items := make([]string, 0, len(tasks)+1)
	for _, task := range tasks {
		var icon string
		end := task.Ended
		switch task.Status {
		case agent.TaskRunning:
			icon = t.Pills.TodoSpinner.Render(taskSpinner[time.Now().Unix()%int64(len(taskSpinner))])
			end = time.Now()
		case agent.TaskDone:
			icon = t.Tool.IconSuccess.Render()
		case agent.TaskFailed:
			icon = t.Tool.IconError.Render()
		default:
			icon = t.Tool.IconCancelled.Render()
		}
		items = append(items, icon+" "+t.Pills.TodoLabel.Render(task.Name)+" "+t.Pills.HelpText.Render(task.CLI+" "+formatElapsed(end.Sub(task.Started))))
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
	case m.taskSel < len(tasks):
		hint = "←/→ select · x stop · enter list · ↑ back"
	default:
		hint = "←/→ select · enter list · ↑ back"
	}
	line := " " + strings.Join(items, " ") + "  " + t.Pills.HelpText.Render(hint)
	return ansi.Truncate(line, width, "…")
}

func formatElapsed(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
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
	if m.taskSel < len(m.visibleTasks()) {
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

// openSubAgentsDialog lists only sub-agents, selecting the one in the row.
func (m *UI) openSubAgentsDialog() tea.Cmd {
	m.tasksFocused = false
	tasks := m.visibleTasks()
	var selected string
	if m.taskSel < len(tasks) {
		selected = tasks[m.taskSel].ID
	}
	m.dialog.CloseDialog(dialog.BackgroundID)
	m.dialog.CloseDialog(dialog.SubAgentsID)
	m.dialog.OpenDialog(dialog.NewSubAgents(m.com, tasks, selected))
	return nil
}
