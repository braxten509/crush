package model

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
)

const (
	// pillHeightWithBorder is the height of a pill including its border.
	pillHeightWithBorder = 3
	// maxTaskDisplayLength is the maximum length of a task name in the pill.
	maxTaskDisplayLength = 40
)

// pillSection represents which section of the pills panel is focused.
type pillSection int

const (
	pillSectionTodos pillSection = iota
	pillSectionQueue
)

// hasIncompleteTodos returns true if there are any non-completed todos.
func hasIncompleteTodos(todos []session.Todo) bool {
	return session.HasIncompleteTodos(todos)
}

// hasInProgressTodo returns true if there is at least one in-progress todo.
func hasInProgressTodo(todos []session.Todo) bool {
	for _, todo := range todos {
		if todo.Status == session.TodoStatusInProgress {
			return true
		}
	}
	return false
}

// queuePill renders the queue count pill with gradient triangles. Pills always
// render with a border; focus within the expanded panel is conveyed by the list
// shown below the pills, not by hiding a pill's border.
func queuePill(queue int, t *styles.Styles) string {
	if queue <= 0 {
		return ""
	}
	triangles := styles.ForegroundGrad(t.Pills.QueueIconBase, "▶▶▶▶▶▶▶▶▶", false, t.Pills.QueueGradFromColor, t.Pills.QueueGradToColor)
	if queue < len(triangles) {
		triangles = triangles[:queue]
	}

	text := t.Pills.QueueLabel.Render(fmt.Sprintf("%d Queued", queue))
	content := fmt.Sprintf("%s %s", strings.Join(triangles, ""), text)
	return t.Pills.Focused.Render(content)
}

// todoPill renders the todo progress pill with optional spinner and task name.
func todoPill(todos []session.Todo, spinnerView string, panelFocused bool, t *styles.Styles) string {
	if !hasIncompleteTodos(todos) {
		return ""
	}

	completed := 0
	var currentTodo *session.Todo
	for i := range todos {
		switch todos[i].Status {
		case session.TodoStatusCompleted:
			completed++
		case session.TodoStatusInProgress:
			if currentTodo == nil {
				currentTodo = &todos[i]
			}
		}
	}

	total := len(todos)

	label := t.Pills.TodoLabel.Render("To-Do")
	progress := t.Pills.TodoProgress.Render(fmt.Sprintf("%d/%d", completed, total))

	var content string
	if panelFocused {
		content = fmt.Sprintf("%s %s", label, progress)
	} else if currentTodo != nil {
		taskText := currentTodo.Content
		if currentTodo.ActiveForm != "" {
			taskText = currentTodo.ActiveForm
		}
		if ansi.StringWidth(taskText) > maxTaskDisplayLength {
			taskText = ansi.Truncate(taskText, maxTaskDisplayLength-1, "…")
		}
		task := t.Pills.TodoCurrentTask.Render(taskText)
		content = fmt.Sprintf("%s %s %s  %s", spinnerView, label, progress, task)
	} else {
		content = fmt.Sprintf("%s %s", label, progress)
	}

	return t.Pills.Focused.Render(content)
}

// todoList renders the expanded todo list.
func todoList(sessionTodos []session.Todo, spinnerView string, t *styles.Styles, width int) string {
	return chat.FormatTodosList(t, sessionTodos, spinnerView, width)
}

// queueList renders the expanded queue items list.
func queueList(queueItems []string, t *styles.Styles, width int) string {
	if len(queueItems) == 0 {
		return ""
	}

	var lines []string
	for _, item := range queueItems {
		text := item
		if name, status, ok := agent.ParseTaskNotification(item); ok {
			text = "Result of task " + name
			if name == agent.AskName && (status == agent.AskAnswered || status == agent.AskCancelled) {
				text = "Your answers to the questions"
			}
		}
		prefix := t.Pills.QueueItemPrefix.Render() + " "
		// One line per message: pillsAreaHeight counts one row each.
		text = strings.Join(strings.Fields(text), " ")
		if room := max(width-ansi.StringWidth(prefix), 1); ansi.StringWidth(text) > room {
			text = ansi.Truncate(text, room-1, "…")
		}
		lines = append(lines, prefix+t.Pills.QueueItemText.Render(text))
	}

	return strings.Join(lines, "\n")
}

// pillsHeightReasonableTerminalHeight is the minimum terminal height at which
// we auto-expand pills when there are incomplete todos.
const pillsHeightReasonableTerminalHeight = 40

// autoExpandPillsIfReasonable expands the pills panel if the terminal has
// enough vertical space to show the expanded list comfortably.
func (m *UI) autoExpandPillsIfReasonable() tea.Cmd {
	if !m.hasSession() {
		return nil
	}
	if m.activeInline != nil {
		return nil
	}
	if m.height < pillsHeightReasonableTerminalHeight {
		return nil
	}
	hasPills := hasIncompleteTodos(m.session.Todos) || m.visiblePromptQueueCount() > 0
	if !hasPills {
		return nil
	}
	if m.pillsExpanded {
		return nil
	}
	if m.pillsAutoExpanded {
		return nil
	}
	m.pillsExpanded = true
	m.pillsAutoExpanded = true
	if hasIncompleteTodos(m.session.Todos) {
		m.focusedPillSection = pillSectionTodos
	} else {
		m.focusedPillSection = pillSectionQueue
	}
	m.updateLayoutAndSize()
	if m.chat.Follow() {
		m.chat.ScrollToBottom()
	}
	return nil
}

// togglePillsExpanded toggles the pills panel expansion state.
func (m *UI) togglePillsExpanded() tea.Cmd {
	if !m.hasSession() {
		return nil
	}
	hasPills := hasIncompleteTodos(m.session.Todos) || m.visiblePromptQueueCount() > 0
	if !hasPills {
		return nil
	}
	m.pillsExpanded = !m.pillsExpanded
	if m.pillsExpanded {
		if hasIncompleteTodos(m.session.Todos) {
			m.focusedPillSection = pillSectionTodos
		} else {
			m.focusedPillSection = pillSectionQueue
		}
	}
	m.updateLayoutAndSize()

	// Make sure to follow scroll if follow is enabled when toggling pills.
	// Note: uses ScrollToBottom (no scrollbar) since this is layout adjustment,
	// not user-initiated scrolling.
	if m.chat.Follow() {
		m.chat.ScrollToBottom()
	}

	return nil
}

// switchPillSection changes focus between todo and queue sections.
func (m *UI) switchPillSection(dir int) tea.Cmd {
	if !m.pillsExpanded || !m.hasSession() {
		return nil
	}
	hasIncompleteTodos := hasIncompleteTodos(m.session.Todos)
	hasQueue := m.visiblePromptQueueCount() > 0

	if dir < 0 && m.focusedPillSection == pillSectionQueue && hasIncompleteTodos {
		m.focusedPillSection = pillSectionTodos
		m.updateLayoutAndSize()
		return nil
	}
	if dir > 0 && m.focusedPillSection == pillSectionTodos && hasQueue {
		m.focusedPillSection = pillSectionQueue
		m.updateLayoutAndSize()
		return nil
	}
	return nil
}

// effectiveFocusedSection returns the pill section that should be treated as
// focused for rendering. The stored focusedPillSection can go stale when its
// section loses all content (for example todos complete while the panel is open,
// or it defaults to todos before any todos exist). In that case we fall through
// to whichever section still has content so the expanded list stays populated.
func (m *UI) effectiveFocusedSection() pillSection {
	hasIncomplete := hasIncompleteTodos(m.session.Todos)
	hasQueue := m.visiblePromptQueueCount() > 0
	switch m.focusedPillSection {
	case pillSectionQueue:
		if hasQueue {
			return pillSectionQueue
		}
		if hasIncomplete {
			return pillSectionTodos
		}
	default: // pillSectionTodos
		if hasIncomplete {
			return pillSectionTodos
		}
		if hasQueue {
			return pillSectionQueue
		}
	}
	return m.focusedPillSection
}

// pillsAreaHeight calculates the total height needed for the pills area.
func (m *UI) pillsAreaHeight() int {
	if !m.hasSession() {
		return 0
	}
	// Suppress pills when an inline editor (e.g. question form) is active
	// to avoid competing for screen space.
	if m.activeInline != nil {
		return 0
	}
	hasIncomplete := hasIncompleteTodos(m.session.Todos)
	hasQueue := m.visiblePromptQueueCount() > 0
	hasPills := hasIncomplete || hasQueue
	if !hasPills {
		return 0
	}

	pillsAreaHeight := pillHeightWithBorder
	if m.pillsExpanded && hasIncomplete && m.effectiveFocusedSection() == pillSectionTodos {
		pillsAreaHeight += len(m.session.Todos)
	}
	// Queued messages are always listed, so a message sent mid-turn stays
	// in sight until it joins the chat.
	pillsAreaHeight += len(m.visiblePromptQueueItems())
	return pillsAreaHeight
}

// renderPills renders the pills panel and stores it in m.pillsView.
func (m *UI) renderPills() {
	m.pillsView = ""
	if !m.hasSession() {
		return
	}
	// Suppress pills when an inline editor (e.g. question form) is active.
	if m.activeInline != nil {
		return
	}

	width := m.layout.pills.Dx()
	if width <= 0 {
		return
	}

	paddingLeft := 3
	contentWidth := max(width-paddingLeft, 0)

	hasIncomplete := hasIncompleteTodos(m.session.Todos)
	hasQueue := m.visiblePromptQueueCount() > 0

	if !hasIncomplete && !hasQueue {
		return
	}

	t := m.com.Styles
	effective := m.effectiveFocusedSection()
	todosFocused := m.pillsExpanded && effective == pillSectionTodos

	inProgressIcon := t.Tool.TodoInProgressIcon.Render(styles.SpinnerIcon)
	if m.todoIsSpinning {
		inProgressIcon = m.todoSpinner.View()
	}

	var pills []string
	if hasIncomplete {
		pills = append(pills, todoPill(m.session.Todos, inProgressIcon, m.pillsExpanded, t))
	}
	if hasQueue {
		pills = append(pills, queuePill(m.visiblePromptQueueCount(), t))
	}

	var lists []string
	if todosFocused && hasIncomplete {
		lists = append(lists, todoList(m.session.Todos, inProgressIcon, t, contentWidth))
	}
	// Render from the memoized queue (fetched off-thread, see
	// workspace_cache.go): renderPills runs on the Update/View path and
	// must never block on a workspace round-trip.
	if list := queueList(m.visiblePromptQueueItems(), t, contentWidth); list != "" {
		lists = append(lists, list)
	}

	if len(pills) == 0 {
		return
	}

	pillsRow := lipgloss.JoinHorizontal(lipgloss.Top, pills...)

	// ctrl+t only opens and closes the to-do list; queued messages are
	// always listed.
	if hasIncomplete {
		helpDesc := "open"
		if m.pillsExpanded {
			helpDesc = "close"
		}
		helpKey := t.Pills.HelpKey.Render("ctrl+t")
		helpText := t.Pills.HelpText.Render(helpDesc)
		helpHint := lipgloss.JoinHorizontal(lipgloss.Center, helpKey, " ", helpText)
		pillsRow = lipgloss.JoinHorizontal(lipgloss.Center, pillsRow, " ", helpHint)
	}

	pillsArea := lipgloss.JoinVertical(lipgloss.Left, append([]string{pillsRow}, lists...)...)

	m.pillsView = t.Pills.Area.MaxWidth(width).PaddingLeft(paddingLeft).Render(pillsArea)
}
