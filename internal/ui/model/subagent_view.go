package model

import (
	"context"
	"image"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/util"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/ultraviolet/screen"
	"github.com/charmbracelet/x/ansi"
)

// subagentView owns only the inspected transcript. The parent's session,
// composer, model, and live updates stay intact while this view is open.
type subagentView struct {
	task          agent.Task
	chat          *Chat
	session       *session.Session
	cancel        context.CancelFunc
	ctx           context.Context
	loading       bool
	dirty         bool
	refreshQueued bool
	returnFocused bool
}

type subagentRefreshMsg struct{ view *subagentView }

type subagentClickMsg struct {
	view *subagentView
	msg  tea.Msg
}

type subagentLoadedMsg struct {
	view     *subagentView
	session  session.Session
	prepared *preparedTranscript
	err      error
}

func (m *UI) openSubagentView(task agent.Task) tea.Cmd {
	if !m.hasSession() || task.SessionID != m.session.ID || task.ChildID == "" || m.activeInline != nil {
		return nil
	}
	m.closeSubagentView()
	ctx, cancel := context.WithCancel(context.Background())
	v := &subagentView{task: task, ctx: ctx, cancel: cancel, chat: NewChat(m.com, config.ScrollbarDefault)}
	v.chat.Focus()
	m.subagentView = v
	m.dialog.CloseDialog(dialog.SubAgentsID)
	m.frame.invalidate()
	return m.loadSubagentView()
}

func (m *UI) closeSubagentView() {
	if v := m.subagentView; v != nil {
		v.cancel()
		m.subagentView = nil
		m.frame.invalidate()
	}
}

func (m *UI) loadSubagentView() tea.Cmd {
	v := m.subagentView
	v.loading, v.dirty, v.refreshQueued = true, false, false
	ws, sty, ctx, id := m.com.Workspace, m.com.Styles, v.ctx, v.task.ChildID
	return func() tea.Msg {
		sess, err := ws.GetSession(ctx, id)
		if err != nil {
			return subagentLoadedMsg{view: v, err: err}
		}
		msgs, err := ws.ListMessages(ctx, id)
		if err != nil {
			return subagentLoadedMsg{view: v, err: err}
		}
		prepared := prepareTranscript(ctx, ws, sty, ws.Config(), ws.WorkingDir(), msgs, nil)
		return subagentLoadedMsg{view: v, session: sess, prepared: prepared}
	}
}

// queueSubagentRefresh coalesces streaming events and never overlaps reads.
func (m *UI) queueSubagentRefresh() tea.Cmd {
	v := m.subagentView
	v.dirty = true
	if v.loading || v.refreshQueued {
		return nil
	}
	v.refreshQueued = true
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return subagentRefreshMsg{view: v} })
}

func (m *UI) handleSubagentView(msg tea.Msg) (tea.Cmd, bool) {
	v := m.subagentView
	switch msg := msg.(type) {
	case subagentClickMsg:
		if v != nil && msg.view == v {
			if click, ok := msg.msg.(DelayedClickMsg); ok {
				v.chat.HandleDelayedClick(click)
				return m.takeChatLink(v.chat), true
			}
		}
		return nil, true
	case subagentRefreshMsg:
		if v == nil || msg.view != v {
			return nil, true
		}
		return m.loadSubagentView(), true
	case subagentLoadedMsg:
		if v == nil || msg.view != v {
			return nil, true
		}
		v.loading = false
		if msg.err != nil {
			m.closeSubagentView()
			return util.ReportError(msg.err), true
		}
		follow, selected := v.chat.Follow(), v.chat.Selected()
		index, line := v.chat.ScrollPosition()
		first := v.session == nil
		v.session = &msg.session
		// Spinners move while the sub-agent runs; a finished one's
		// leftovers stay still.
		v.chat.SetAnimationsAllowed(v.task.Status == agent.TaskRunning)
		// Reuse tool groups so expanding one survives incoming chunks.
		v.chat.flat = slices.Clone(msg.prepared.items)
		v.chat.regroup()
		// Between steps a running sub-agent is still thinking, as in the
		// main chat.
		v.chat.SetAgentBusy(v.task.Status == agent.TaskRunning)
		if first || follow {
			v.chat.ScrollToBottom()
		} else {
			v.chat.list.ScrollToIndex(index)
			v.chat.list.ScrollBy(line)
			v.chat.follow = false
			v.chat.SetSelected(selected)
		}
		m.frame.invalidate()
		if v.dirty {
			return m.queueSubagentRefresh(), true
		}
		return nil, true
	}
	if v == nil {
		return nil, false
	}
	if m.activeInline != nil || m.currentSessionID() != v.task.SessionID {
		m.closeSubagentView()
		return nil, false
	}
	switch msg := msg.(type) {
	case pubsub.Event[message.Message]:
		if msg.Payload.SessionID == v.task.ChildID {
			return m.queueSubagentRefresh(), false
		}
	case pubsub.Event[session.Session]:
		if msg.Payload.ID == v.task.ChildID {
			return m.queueSubagentRefresh(), false
		}
	case pubsub.Event[agent.Task]:
		if msg.Payload.ID == v.task.ID {
			v.task = msg.Payload
			return m.queueSubagentRefresh(), false
		}
	}
	// Dialogs over the view take its input.
	if m.inputOwner() != ownerSubagentView {
		return nil, false
	}
	return m.handleSubagentInput(v, msg)
}

// handleSubagentInput handles the keys and the mouse over the sub-agent
// view. ok is false for any other message.
func (m *UI) handleSubagentInput(v *subagentView, msg tea.Msg) (cmd tea.Cmd, ok bool) {
	main, _, back := m.subagentViewAreas(image.Rect(0, 0, m.width, m.height))
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleSubagentKey(v, msg, main), true
	case tea.KeyReleaseMsg, tea.PasteMsg:
		return nil, true
	case tea.MouseWheelMsg:
		if msg.Button == uv.MouseWheelUp {
			v.chat.ScrollBy(-3)
		} else if msg.Button == uv.MouseWheelDown {
			v.chat.ScrollBy(3)
		}
		return nil, true
	case tea.MouseClickMsg:
		if msg.Button == uv.MouseLeft && image.Pt(msg.X, msg.Y).In(back) {
			m.closeSubagentView()
		} else if msg.Button == uv.MouseLeft && image.Pt(msg.X, msg.Y).In(main) {
			v.returnFocused = false
			if handled, cmd := v.chat.HandleScrollbarPress(msg.X-main.Min.X, msg.Y-main.Min.Y); handled {
				return cmd, true
			}
			_, cmd := v.chat.HandleMouseDown(msg.X-main.Min.X, msg.Y-main.Min.Y)
			if cmd != nil {
				return func() tea.Msg { return subagentClickMsg{view: v, msg: cmd()} }, true
			}
		}
		return nil, true
	case tea.MouseMotionMsg:
		if v.chat.DraggingScrollbar() {
			return v.chat.HandleScrollbarDrag(msg.Y - main.Min.Y), true
		}
		v.chat.HandleMouseDrag(msg.X-main.Min.X, msg.Y-main.Min.Y)
		return nil, true
	case tea.MouseReleaseMsg:
		v.chat.HandleScrollbarRelease()
		v.chat.HandleMouseUp(msg.X-main.Min.X, msg.Y-main.Min.Y)
		return m.takeChatLink(v.chat), true
	}
	return nil, false
}

// handleSubagentAnimTick advances the sub-agent view's spinners. A tick
// from a view that has since closed is dropped, which stops its clock.
func (m *UI) handleSubagentAnimTick(msg animTickMsg) tea.Cmd {
	v := m.subagentView
	if v == nil || v.chat != msg.chat {
		return nil
	}
	changed, cmd := v.chat.Tick(msg)
	if changed {
		if v.chat.Follow() {
			v.chat.ScrollToBottom()
		}
		m.frame.invalidate()
	}
	return cmd
}

// subagentViewLayout puts the sub-agent's chat, read-only composer and
// footer exactly where the main chat puts its own, so opening one looks like
// the main chat.
type subagentViewLayout struct {
	main, composer, usage, tasks, sidebar, back uv.Rectangle
}

func (m *UI) subagentViewLayout(area uv.Rectangle) subagentViewLayout {
	l := m.generateLayout(area.Dx(), area.Dy())
	main := l.main
	if l.pills.Dy() > 0 {
		// The parent's to-do pills aren't the sub-agent's; its chat takes
		// their rows.
		main.Max.Y = max(main.Max.Y, l.pills.Max.Y-1)
	}
	back := l.status
	back.Min.X = l.editor.Min.X + footerIndent
	back.Max.X = min(back.Max.X, back.Min.X+lipgloss.Width(subagentBackLabel))
	return subagentViewLayout{main: main, composer: l.editor, usage: l.usage, tasks: l.tasks, sidebar: l.sidebar, back: back}
}

const subagentBackLabel = "← Return to parent  esc"

func (m *UI) subagentViewAreas(area uv.Rectangle) (main, composer, back uv.Rectangle) {
	l := m.subagentViewLayout(area)
	return l.main, l.composer, l.back
}

// subagentComposer is the read-only stand-in for the composer: the same band
// as the main chat's, holding the sub-agent's name and state.
func (m *UI) subagentComposer(height, width int) string {
	v, t := m.subagentView, m.com.Styles
	label := v.task.Name + " · " + string(v.task.Status)
	if !m.isCompact && v.task.Model != "" {
		// The sidebar shows the parent's model, so name the sub-agent's here.
		label += " · " + v.task.Model
	}
	label += " · read only"
	bg := t.Editor.Textarea.Blurred.Base.GetBackground()
	// An attachments row above and a margin row below, as in renderEditorView.
	lines := []string{""}
	for i := range max(3, height-2) {
		line := ""
		if i == 1 {
			line = ansi.Truncate(t.Editor.PromptNormalIconBlurred.Render()+t.Editor.Textarea.Blurred.Placeholder.Render(label), width, "…")
		}
		lines = append(lines, common.OnBand(line, width, bg))
	}
	return strings.Join(lines, "\n")
}

func (m *UI) drawSubagentView(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	v, t := m.subagentView, m.com.Styles
	screen.Clear(scr)
	l := m.subagentViewLayout(area)
	if !m.isCompact && l.sidebar.Dx() > 0 {
		m.drawSidebar(scr, l.sidebar)
	}
	v.chat.SetSize(l.main.Dx(), l.main.Dy())
	if v.session == nil {
		uv.NewStyledString(t.Pills.HelpText.Render("Opening conversation…")).Draw(scr, l.main)
	} else {
		v.chat.Draw(scr, l.main)
	}
	width := l.composer.Dx()
	if w := v.chat.ListWidth(); w > 0 {
		// The band ends where the sent messages' bands do.
		width = min(width, w)
	}
	uv.NewStyledString(m.subagentComposer(l.composer.Dy(), width)).Draw(scr, l.composer)
	if l.usage.Dy() > 0 {
		pad := strings.Repeat(" ", footerIndent)
		footer := pad + strings.ReplaceAll(m.subagentFooter(l.usage.Dx()-footerIndent), "\n", "\n"+pad)
		uv.NewStyledString(footer).Draw(scr, l.usage)
	}
	if l.tasks.Dy() > 0 {
		uv.NewStyledString(m.renderTaskLine(l.tasks.Dx(), false)).Draw(scr, l.tasks)
	}
	button := t.ComposerFooter.Accent.Underline(v.returnFocused).Render("← Return to parent")
	button += t.ComposerFooter.Text.Render("  esc")
	back := l.back
	back.Max.X = l.composer.Max.X
	uv.NewStyledString(ansi.Truncate(button, back.Dx(), "…")).Draw(scr, back)
	if m.dialog.HasDialogs() {
		return m.dialog.Draw(scr, area)
	}
	return nil
}

// handleSubagentKey handles a key over the sub-agent view, which shows in
// main. The view takes every key.
func (m *UI) handleSubagentKey(v *subagentView, msg tea.KeyPressMsg, main image.Rectangle) tea.Cmd {
	switch msg.String() {
	case "ctrl+c", "y":
		if v.chat.HasHighlight() {
			return common.CopyToClipboardWithCallback(v.chat.HighlightContent(), "Selected text copied to clipboard", nil)
		}
	case "esc":
		m.closeSubagentView()
	case "tab", "shift+tab", "left", "right":
		v.returnFocused = !v.returnFocused
	case "enter", "space":
		if v.returnFocused {
			m.closeSubagentView()
		} else {
			v.chat.ToggleExpandedSelectedItem()
		}
	case "up", "k":
		v.returnFocused = false
		v.chat.ScrollBy(-1)
	case "down", "j":
		if v.chat.AtBottom() {
			v.returnFocused = true
		} else {
			v.chat.ScrollBy(1)
		}
	case "pgup", "ctrl+u":
		v.chat.ScrollBy(-max(1, main.Dy()/2))
	case "pgdown", "ctrl+d":
		v.chat.ScrollBy(max(1, main.Dy()/2))
	case "home":
		v.chat.ScrollToTop()
	case "end":
		v.chat.ScrollToBottom()
	}
	return nil
}
