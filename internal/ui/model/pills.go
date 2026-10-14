package model

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
)

const (
	// todoIndent puts the to-do section's text in the composer text's
	// column, after its prompt mark.
	todoIndent = 2
	// todoRightMargin keeps the ctrl+t hint off the right edge.
	todoRightMargin = 1
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

// todoHeader renders the to-do section's label row: "TO-DO 1/4", while
// closed the step in progress, and the ctrl+t hint at the right edge.
func todoHeader(todos []session.Todo, spinnerView string, expanded bool, t *styles.Styles, width int) string {
	completed := 0
	var current *session.Todo
	for i := range todos {
		switch todos[i].Status {
		case session.TodoStatusCompleted:
			completed++
		case session.TodoStatusInProgress:
			if current == nil {
				current = &todos[i]
			}
		}
	}
	left := t.Pills.TodoLabel.Render("TO-DO") + " " + t.Pills.TodoProgress.Render(fmt.Sprintf("%d/%d", completed, len(todos)))
	hintDesc := "hide"
	if !expanded {
		hintDesc = "show"
		if current != nil {
			task := current.Content
			if current.ActiveForm != "" {
				task = current.ActiveForm
			}
			left += t.Pills.TodoProgress.Render(" · ") + spinnerView + " " + t.Pills.TodoCurrentTask.Render(task)
		}
	}
	hint := t.Pills.HelpKey.Render("ctrl+t") + " " + t.Pills.HelpText.Render(hintDesc)
	room := width - ansi.StringWidth(hint) - 2
	if ansi.StringWidth(left) > room {
		left = ansi.Truncate(left, max(room, 0), "…")
	}
	return left + strings.Repeat(" ", max(width-ansi.StringWidth(left)-ansi.StringWidth(hint), 1)) + hint
}

// todoList renders the expanded todo list.
func todoList(sessionTodos []session.Todo, spinnerView string, t *styles.Styles, width int) string {
	return chat.FormatTodosList(t, sessionTodos, spinnerView, width)
}

// queueRows renders the queued messages as dim rows on the composer's band
// width, right above it, where they will be sent from. The first says it is
// waiting and goes with the next step.
func queueRows(queueItems []string, t *styles.Styles, width int) []string {
	var rows []string
	for i, item := range queueItems {
		text := item
		if name, status, ok := agent.ParseTaskNotification(item); ok {
			text = "Result of task " + name
			if status == string(agent.TaskRunning) {
				text = "Progress of " + name
			}
			if name == agent.AskName && (status == agent.AskAnswered || status == agent.AskCancelled) {
				text = "Your answers to the questions"
			}
		}
		prefix := t.Pills.QueueItemPrefix.Render() + " "
		hint := ""
		if i == 0 {
			hint = t.Pills.QueueItemHint.Render("waiting · sends next") + " "
		}
		// One line per message: the composer counts one row each.
		text = strings.Join(strings.Fields(text), " ")
		if room := max(width-ansi.StringWidth(prefix)-ansi.StringWidth(hint)-2, 1); ansi.StringWidth(text) > room {
			text = ansi.Truncate(text, room-1, "…")
		}
		row := prefix + t.Pills.QueueItemText.Render(text)
		if hint != "" {
			row += strings.Repeat(" ", max(width-ansi.StringWidth(row)-ansi.StringWidth(hint), 1)) + hint
		}
		rows = append(rows, common.OnBand(row, width, t.Pills.QueueRowBg))
	}
	return rows
}

// queueRowCount is how many queued message rows the composer shows above
// its band; none while an inline editor (e.g. the question form) has its
// place.
func (m *UI) queueRowCount() int {
	if !m.hasSession() || m.activeInline != nil {
		return 0
	}
	return len(m.visiblePromptQueueItems())
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
	if !hasIncompleteTodos(m.session.Todos) {
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
	if !hasIncompleteTodos(m.session.Todos) {
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

// pillsAreaHeight is the to-do section's height: its label row, then the
// list while open. Queued messages are drawn with the composer instead.
func (m *UI) pillsAreaHeight() int {
	if !m.hasSession() {
		return 0
	}
	// Suppress pills when an inline editor (e.g. question form) is active
	// to avoid competing for screen space.
	if m.activeInline != nil {
		return 0
	}
	if !hasIncompleteTodos(m.session.Todos) {
		return 0
	}
	height := 1
	if m.pillsExpanded {
		height += len(m.session.Todos)
	}
	// The blank row between the to-dos and the composer holds the
	// attachments when there are any; keep one free row above them.
	if m.hasAttachments() {
		height++
	}
	return height
}

func (m *UI) hasAttachments() bool {
	return m.attachments != nil && len(m.attachments.List()) > 0
}

// syncAttachmentsLayout lays out again when attachments came or went, since
// they add a row under the to-dos. Any handler may have changed them.
func (m *UI) syncAttachmentsLayout() {
	if m.hasAttachments() != m.layoutAttachments {
		m.updateLayoutAndSize()
	}
}

// renderPills renders the to-do section and stores it in m.pillsView. Its
// text starts at the composer text's column.
func (m *UI) renderPills() {
	m.pillsView = ""
	if !m.hasSession() || m.activeInline != nil || !hasIncompleteTodos(m.session.Todos) {
		return
	}
	width := m.layout.pills.Dx()
	if width <= 0 {
		return
	}
	contentWidth := max(width-todoIndent-todoRightMargin, 0)

	t := m.com.Styles
	inProgressIcon := t.Tool.TodoInProgressIcon.Render(styles.SpinnerIcon)
	if m.todoIsSpinning {
		inProgressIcon = m.todoSpinner.View()
	}
	rows := []string{todoHeader(m.session.Todos, inProgressIcon, m.pillsExpanded, t, contentWidth)}
	if m.pillsExpanded {
		rows = append(rows, todoList(m.session.Todos, inProgressIcon, t, contentWidth))
	}
	m.pillsView = t.Pills.Area.MaxWidth(width).PaddingLeft(todoIndent).Render(strings.Join(rows, "\n"))
}
