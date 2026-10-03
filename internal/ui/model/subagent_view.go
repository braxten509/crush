package model

import (
	"context"
	"image"
	"slices"
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
	m.invalidateFrames()
	return m.loadSubagentView()
}

func (m *UI) closeSubagentView() {
	if v := m.subagentView; v != nil {
		v.cancel()
		m.subagentView = nil
		m.invalidateFrames()
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
		if first || follow {
			v.chat.ScrollToBottom()
		} else {
			v.chat.list.ScrollToIndex(index)
			v.chat.list.ScrollBy(line)
			v.chat.follow = false
			v.chat.SetSelected(selected)
		}
		m.invalidateFrames()
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
	if m.dialog.HasDialogs() {
		return nil, false
	}
	main, _, back := m.subagentViewAreas(image.Rect(0, 0, m.width, m.height))
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "y":
			if v.chat.HasHighlight() {
				return common.CopyToClipboardWithCallback(v.chat.HighlightContent(), "Selected text copied to clipboard", nil), true
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
		return nil, true
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
		} else if image.Pt(msg.X, msg.Y).In(main) {
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
		return nil, true
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
		m.invalidateFrames()
	}
	return cmd
}

func (m *UI) subagentViewAreas(area uv.Rectangle) (main, composer, back uv.Rectangle) {
	inner := image.Rect(area.Min.X+1, area.Min.Y+1, max(area.Min.X+1, area.Max.X-1), max(area.Min.Y+1, area.Max.Y-1))
	back = inner
	back.Min.Y = max(inner.Min.Y, inner.Max.Y-1)
	composer = inner
	composer.Max.Y = max(inner.Min.Y, back.Min.Y-1)
	footerHeight := lipgloss.Height(m.subagentFooter(inner.Dx()))
	composer.Min.Y = max(inner.Min.Y, composer.Max.Y-1-footerHeight)
	main = inner
	main.Min.Y = min(inner.Max.Y, inner.Min.Y+2)
	main.Max.Y = max(main.Min.Y, composer.Min.Y-1)
	return main, composer, back
}

func (m *UI) drawSubagentView(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	v, t := m.subagentView, m.com.Styles
	screen.Clear(scr)
	main, composer, back := m.subagentViewAreas(area)
	title := "Sub-agent: " + v.task.Name + " · " + string(v.task.Status)
	uv.NewStyledString(t.Pills.TodoLabel.Render(ansi.Truncate(title, main.Dx(), "…"))).Draw(scr, image.Rect(main.Min.X, area.Min.Y+1, main.Max.X, area.Min.Y+2))
	v.chat.SetSize(main.Dx(), main.Dy())
	if v.session == nil {
		uv.NewStyledString(t.Pills.HelpText.Render("Opening conversation…")).Draw(scr, main)
	} else {
		v.chat.Draw(scr, main)
	}
	label := t.Pills.HelpText.Render("Viewing sub-agent · read only")
	uv.NewStyledString(ansi.Truncate(label, composer.Dx(), "…")).Draw(scr, composer)
	composer.Min.Y++
	uv.NewStyledString(m.subagentFooter(composer.Dx())).Draw(scr, composer)
	button := t.ComposerFooter.Accent.Underline(v.returnFocused).Render("← Return to parent")
	button += t.ComposerFooter.Text.Render("  esc")
	uv.NewStyledString(ansi.Truncate(button, back.Dx(), "…")).Draw(scr, back)
	if m.dialog.HasDialogs() {
		return m.dialog.Draw(scr, area)
	}
	return nil
}
