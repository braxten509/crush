package model

import (
	"context"
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
	// bgProcsMsg carries a fresh list of background processes. Inside the
	// session list, servers among them are shown there instead.
	bgProcsMsg struct {
		procs   []agent.Process
		servers []agent.Server
	}
)

// taskSpinner is the half circle that turns while sub-agents run.
var taskSpinner = []string{"◐", "◓", "◑", "◒"}

// taskRow is the row under the editor listing the background sub-agents and
// processes, and its selection.
type taskRow struct {
	// tasks are the background sub-agents seen this run, by ID, and
	// bgProcs the processes the agents left running.
	tasks   map[string]agent.Task
	ticking bool
	bgProcs []agent.Process
	// hostServers are background processes listening on a port, which the
	// session list shows in place of the row.
	hostServers []agent.Server
	// focused is set while Down from the editor selected the row; sel is
	// the selected category.
	focused bool
	sel     int
}

// visible returns the running sub-agents of sessionID, oldest first.
func (r *taskRow) visible(sessionID string) []agent.Task {
	if sessionID == "" {
		return nil
	}
	var out []agent.Task
	for _, t := range r.tasks {
		if t.SessionID == sessionID && t.Status == agent.TaskRunning {
			out = append(out, t)
		}
	}
	slices.SortFunc(out, func(a, b agent.Task) int { return a.Started.Compare(b.Started) })
	return out
}

// len is how many categories the row has for sessionID: sub-agents, then
// processes, if any.
func (r *taskRow) len(sessionID string) int {
	n := 0
	if len(r.visible(sessionID)) > 0 {
		n++
	}
	if len(r.bgProcs) > 0 {
		n++
	}
	return n
}

// record stores the latest state of a sub-agent.
func (r *taskRow) record(t agent.Task) {
	if r.tasks == nil {
		r.tasks = map[string]agent.Task{}
	}
	r.tasks[t.ID] = t
}

// expire drops the sub-agents that finished more than taskLinger ago.
func (r *taskRow) expire() {
	for id, t := range r.tasks {
		if t.Status != agent.TaskRunning && time.Since(t.Ended) >= taskLinger {
			delete(r.tasks, id)
		}
	}
}

// fit keeps the selection within a row of n categories, dropping the focus
// when the row is empty.
func (r *taskRow) fit(n int) {
	if n == 0 {
		r.focused = false
	}
	r.sel = min(r.sel, max(n-1, 0))
}

// visibleTasks returns the running sub-agents of the current session.
func (m *UI) visibleTasks() []agent.Task {
	return m.taskRow.visible(m.currentSessionID())
}

// taskRowLen is how many categories the row has: sub-agents, then
// processes, if any.
func (m *UI) taskRowLen() int {
	return m.taskRow.len(m.currentSessionID())
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
	m.taskRow.fit(m.taskRowLen())
	if m.tasksHeight() != m.layout.tasks.Dy() {
		m.updateLayoutAndSize()
	}
}

func (m *UI) handleTaskEvent(t agent.Task) tea.Cmd {
	m.taskRow.record(t)
	m.relayoutTasks()
	m.refreshBackgroundDialog()
	return m.taskTick()
}

// taskTick schedules the next refresh while any sub-agent is listed.
func (m *UI) taskTick() tea.Cmd {
	if m.taskRow.ticking || len(m.visibleTasks()) == 0 {
		return nil
	}
	m.taskRow.ticking = true
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return taskTickMsg{} })
}

func (m *UI) handleTaskTick() tea.Cmd {
	m.taskRow.ticking = false
	m.taskRow.expire()
	m.relayoutTasks()
	return m.taskTick()
}

// pollBgProcs lists the background processes off the update loop.
func (m *UI) pollBgProcs() tea.Cmd {
	inHost := m.inSessionHost
	com, sessionID := m.com, m.currentSessionID()
	return tea.Tick(bgProcsRefresh, func(time.Time) tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		procs := com.BackgroundJobs(ctx, sessionID)
		if !inHost {
			return bgProcsMsg{procs: procs}
		}
		servers, rest := agent.FindServers(procs)
		return bgProcsMsg{procs: rest, servers: servers}
	})
}

func (m *UI) handleBgProcs(msg bgProcsMsg) tea.Cmd {
	m.taskRow.bgProcs, m.taskRow.hostServers = msg.procs, msg.servers
	m.relayoutTasks()
	m.refreshBackgroundDialog()
	return m.pollBgProcs()
}

// refreshBackgroundDialog keeps an open background dialog in step with
// what is actually running.
func (m *UI) refreshBackgroundDialog() {
	for _, id := range []string{dialog.BackgroundID, dialog.SubAgentsID} {
		if d, ok := m.dialog.Dialog(id).(*dialog.Background); ok {
			d.SetItems(m.visibleTasks(), m.taskRow.bgProcs, "")
		}
	}
}

// focusTaskRow selects the row from the editor, if it has anything.
func (m *UI) focusTaskRow() bool {
	return m.taskRow.focus(m.taskRowLen())
}

// focus selects a row of n categories, if it has any.
func (r *taskRow) focus(n int) bool {
	if n == 0 {
		return false
	}
	r.focused = true
	r.sel = min(r.sel, n-1)
	return true
}

// handleTaskRowKey handles keys while the row is selected. Anything it
// doesn't use gives focus back to the editor and is handled there.
func (m *UI) handleTaskRowKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	n := m.taskRowLen()
	if n == 0 {
		m.taskRow.focused = false
		return nil, false
	}
	// The background key opens what's selected, like Enter.
	if key.Matches(msg, m.keyMap.Chat.Background) {
		return m.openTaskRowDialog(), true
	}
	switch msg.String() {
	case "left":
		m.taskRow.sel = (m.taskRow.sel + n - 1) % n
	case "right", "tab":
		m.taskRow.sel = (m.taskRow.sel + 1) % n
	case "up", "esc":
		m.taskRow.focused = false
	case "enter", "space", "x", "delete", "backspace":
		return m.openTaskRowDialog(), true
	default:
		m.taskRow.focused = false
		return nil, false
	}
	return nil, true
}

func (m *UI) renderTasks(width int) string {
	return m.renderTaskLine(width, true)
}

// renderTaskLine draws the task row; withHint adds the keys that manage it,
// which only work from the main chat.
func (m *UI) renderTaskLine(width int, withHint bool) string {
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
	if n := len(m.taskRow.bgProcs); n > 0 {
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
		if m.taskRow.focused && i == m.taskRow.sel {
			items[i] = t.Dialog.SelectedItem.Render(ansi.Strip(item))
		} else {
			items[i] = " " + item + " "
		}
	}

	var hint string
	switch {
	case !m.taskRow.focused:
		hint = "↓ manage"
	default:
		hint = "enter list · ←/→ select · ↑ back"
	}
	line := " " + strings.Join(items, " ")
	if withHint {
		line += "  " + t.Pills.HelpText.Render(hint)
	}
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
	if m.taskRow.sel == 0 && len(m.visibleTasks()) > 0 {
		return m.openSubAgentsDialog()
	}
	return m.openBackgroundDialog()
}

// openBackgroundDialog lists only background processes.
func (m *UI) openBackgroundDialog() tea.Cmd {
	m.taskRow.focused = false
	m.dialog.CloseDialog(dialog.SubAgentsID)
	m.dialog.CloseDialog(dialog.BackgroundID)
	m.dialog.OpenDialog(dialog.NewBackground(m.com, m.taskRow.bgProcs))
	return nil
}

// openSubAgentsDialog lists only sub-agents.
func (m *UI) openSubAgentsDialog() tea.Cmd {
	m.taskRow.focused = false
	tasks := m.visibleTasks()
	m.dialog.CloseDialog(dialog.BackgroundID)
	m.dialog.CloseDialog(dialog.SubAgentsID)
	m.dialog.OpenDialog(dialog.NewSubAgents(m.com, tasks, ""))
	return nil
}
