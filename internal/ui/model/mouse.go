package model

import (
	"image"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	uv "github.com/charmbracelet/ultraviolet"
)

// Mouse input goes to the dialogs first when any are open, then to the
// region under the pointer: the inline editor, the composer (attachment
// chips and the textarea), the chat or the sidebar. A press or drag a
// region took ends the update early (flowReturn).

// handleMouseClick routes a mouse press.
func (m *UI) handleMouseClick(msg tea.MouseClickMsg) (tea.Cmd, updateFlow) {
	m.imagePress = nil
	// Pass mouse events to dialogs first if any are open.
	if m.dialog.HasDialogs() {
		return m.handleDialogMsg(msg), flowReturn
	}
	// Route clicks to inline editors that support mouse interaction.
	if cmd, handled := m.clickInline(msg); handled {
		return cmd, flowReturn
	}

	cmds := []tea.Cmd{m.handleClickFocus(msg)}
	if cmd, handled := m.clickComposer(msg); handled {
		return tea.Batch(append(cmds, cmd)...), flowReturn
	}
	if m.state == uiChat {
		cmd, handled := m.clickChat(msg)
		cmds = append(cmds, cmd)
		if handled {
			return tea.Batch(cmds...), flowReturn
		}
	}
	return tea.Batch(cmds...), flowContinue
}

// clickInline gives a press to an inline editor that handles the mouse.
func (m *UI) clickInline(msg tea.MouseClickMsg) (tea.Cmd, bool) {
	if m.activeInline == nil {
		return nil, false
	}
	if selectable, ok := m.activeInline.(dialog.MouseSelectableEditor); ok &&
		selectable.HandleMouseDown(msg.X, msg.Y) {
		return nil, true
	}
	if clickable, ok := m.activeInline.(dialog.MouseClickableEditor); ok {
		if done, handled := clickable.HandleMouseClick(msg.X, msg.Y); handled {
			if done {
				return m.closeDoneInline(), true
			}
			return nil, true
		}
	}
	return nil, false
}

// clickComposer handles a press on the composer: an attachment chip's
// remove button or image, which sit right above the composer band, or the
// textarea, which positions the cursor and starts a selection.
func (m *UI) clickComposer(msg tea.MouseClickMsg) (tea.Cmd, bool) {
	if m.activeInline != nil {
		return nil, false
	}
	if msg.Button == uv.MouseLeft && m.attachments.Count() > 0 && msg.Y == m.layout.editor.Min.Y+editorAttachmentsRow {
		relX := msg.X - m.layout.editor.Min.X
		if m.attachments.HandleClick(relX) {
			return m.turnOffRemovedKeptSkills(), true
		}
		if attachment := m.attachments.Renderer().ImageAt(relX); attachment != nil {
			m.imagePress = &imagePress{attachment: *attachment, at: image.Pt(msg.X, msg.Y)}
			return nil, true
		}
	}
	if handled, cmd := m.forwardMouseToTextarea(msg); handled {
		return cmd, true
	}
	return nil, false
}

// clickChat handles a left press on the chat: its scrollbar, or a message.
// handled is set when the scrollbar took the press.
func (m *UI) clickChat(msg tea.MouseClickMsg) (cmd tea.Cmd, handled bool) {
	if msg.Button != uv.MouseLeft || !image.Pt(msg.X, msg.Y).In(m.layout.main) {
		return nil, false
	}
	// Adjust for chat area position
	x, y := msg.X-m.layout.main.Min.X, msg.Y-m.layout.main.Min.Y
	if handled, cmd := m.chat.HandleScrollbarPress(x, y); handled {
		return cmd, true
	}
	if handled, cmd := m.chat.HandleMouseDown(x, y); handled {
		m.lastClickTime = time.Now()
		return cmd, false
	}
	return nil, false
}

// handleMouseMotion routes a mouse move.
func (m *UI) handleMouseMotion(msg tea.MouseMotionMsg) (tea.Cmd, updateFlow) {
	if m.imagePress != nil && image.Pt(msg.X, msg.Y) != m.imagePress.at {
		m.imagePress.dragged = true
	}
	// Pass mouse events to dialogs first if any are open.
	if m.dialog.HasDialogs() {
		m.dialog.Update(msg)
		return nil, flowReturn
	}
	if m.moveOverInline(msg) {
		return nil, flowReturn
	}
	// While a mouse selection is in progress in the textarea, forward
	// motion events to it and skip chat drag handling.
	if m.activeInline == nil && m.textareaMouseSelecting {
		_, cmd := m.forwardMouseToTextarea(msg)
		return cmd, flowReturn
	}
	if m.state == uiChat {
		return m.dragChat(msg)
	}
	return nil, flowContinue
}

// moveOverInline tracks the pointer for an inline editor: a selection drag,
// which it reports, or hover highlighting.
func (m *UI) moveOverInline(msg tea.MouseMotionMsg) (dragged bool) {
	if m.activeInline == nil {
		return false
	}
	if selectable, ok := m.activeInline.(dialog.MouseSelectableEditor); ok &&
		selectable.HandleMouseDrag(msg.X, msg.Y) {
		return true
	}
	if m.hoverX != msg.X || m.hoverY != msg.Y {
		m.hoverX = msg.X
		m.hoverY = msg.Y
		if clickable, ok := m.activeInline.(dialog.MouseClickableEditor); ok {
			clickable.SetHover(msg.X, msg.Y)
		}
	}
	return false
}

// dragChat handles a mouse move over the chat: a scrollbar drag, scrolling
// at the edges while selecting, and extending the selection.
func (m *UI) dragChat(msg tea.MouseMotionMsg) (tea.Cmd, updateFlow) {
	if m.chat.DraggingScrollbar() {
		return m.chat.HandleScrollbarDrag(msg.Y - m.layout.main.Min.Y), flowReturn
	}
	// Skip chat edge-scrolling when an inline editor is active to prevent
	// accidental scrolling while hovering over question forms or other
	// inline components. It is for dragging a selection, so a move with no
	// button held (sent while something asks for every move) never
	// scrolls: that would leave the newest messages out of view.
	if msg.Button != tea.MouseNone && (m.activeInline == nil || m.focus != uiFocusEditor) {
		if msg.Y <= 0 {
			m.chat.ScrollBy(-1)
			if !m.chat.SelectedItemInView() {
				m.chat.SelectPrev()
				m.chat.ScrollToSelected()
			}
		} else if msg.Y >= m.chat.Height()-1 {
			m.chat.ScrollBy(1)
			if !m.chat.SelectedItemInView() {
				m.chat.SelectNext()
				m.chat.ScrollToSelected()
			}
		}
	}

	// Adjust for chat area position
	m.chat.HandleMouseDrag(msg.X-m.layout.main.Min.X, msg.Y-m.layout.main.Min.Y)
	return nil, flowContinue
}

// handleMouseRelease routes a mouse release. endedScrollbarDrag reports
// that the release ended a chat scrollbar drag, which update settles before
// anything else.
func (m *UI) handleMouseRelease(msg tea.MouseReleaseMsg, endedScrollbarDrag bool) (tea.Cmd, updateFlow) {
	if press := m.imagePress; press != nil {
		m.imagePress = nil
		if msg.Button == uv.MouseLeft && !press.dragged && image.Pt(msg.X, msg.Y) == press.at && m.activeInline == nil && !m.dialog.HasDialogs() {
			return func() tea.Msg { return openImageMsg{press.attachment} }, flowReturn
		}
		return nil, flowReturn
	}
	// Pass mouse events to dialogs first if any are open.
	if m.dialog.HasDialogs() {
		m.dialog.Update(msg)
		return nil, flowReturn
	}
	if cmd, handled := m.releaseInline(msg); handled {
		return cmd, flowReturn
	}
	// End any in-progress textarea mouse selection.
	if m.activeInline == nil && m.textareaMouseSelecting {
		m.textareaMouseSelecting = false
		_, cmd := m.forwardMouseToTextarea(msg)
		return cmd, flowReturn
	}
	if m.state != uiChat {
		return nil, flowContinue
	}
	if endedScrollbarDrag {
		return nil, flowReturn
	}
	return m.releaseChat(msg), flowContinue
}

// releaseInline ends a selection in an inline editor that handles the
// mouse, closing the editor when the release finished it.
func (m *UI) releaseInline(msg tea.MouseReleaseMsg) (tea.Cmd, bool) {
	if m.activeInline == nil {
		return nil, false
	}
	selectable, ok := m.activeInline.(dialog.MouseSelectableEditor)
	if !ok {
		return nil, false
	}
	handled, cmd := selectable.HandleMouseRelease(msg.X, msg.Y)
	if !handled {
		return nil, false
	}
	cmds := []tea.Cmd{cmd}
	if rd, ok := m.activeInline.(dialog.ReleaseDoneEditor); ok && rd.TakeReleaseDone() {
		cmds = append(cmds, m.closeDoneInline())
	}
	return tea.Batch(cmds...), true
}

// releaseChat ends a press on the chat: it follows a link under the
// pointer and, a double-click interval after a selection, copies it.
func (m *UI) releaseChat(msg tea.MouseReleaseMsg) tea.Cmd {
	// Adjust for chat area position
	released := m.chat.HandleMouseUp(msg.X-m.layout.main.Min.X, msg.Y-m.layout.main.Min.Y)
	cmds := []tea.Cmd{m.takeChatLink(m.chat)}
	if released && m.chat.HasHighlight() {
		cmds = append(cmds, tea.Tick(doubleClickThreshold, func(time.Time) tea.Msg {
			return chatReleaseCopyMsg{}
		}))
	}
	return tea.Batch(cmds...)
}

// handleMouseWheel routes a wheel event: an inline editor under the pointer
// gets it first, even over a dialog, so scrolling over the chat still
// scrolls the chat.
func (m *UI) handleMouseWheel(msg common.CoalescedWheelMsg) (tea.Cmd, updateFlow) {
	if m.activeInline != nil && image.Pt(msg.Mouse.X, msg.Mouse.Y).In(m.layout.editor) {
		if we, ok := m.activeInline.(common.WheelScrollable); ok {
			we.HandleWheel(msg.DeltaX, msg.DeltaY)
			return nil, flowReturn
		}
	}
	// Pass mouse events to dialogs first if any are open.
	if m.dialog.HasDialogs() {
		m.dialog.Update(msg)
		return nil, flowReturn
	}
	if m.state == uiChat {
		return m.wheelChat(msg), flowContinue
	}
	return nil, flowContinue
}

// wheelChat scrolls the sidebar or the chat. A focused sidebar gets the
// wheel; with composer_focus_only (where it can't be focused) the wheel
// scrolls it while the pointer is over it. The input filter already
// converts wheel events to line counts.
func (m *UI) wheelChat(msg common.CoalescedWheelMsg) tea.Cmd {
	overSidebar := image.Pt(msg.Mouse.X, msg.Mouse.Y).In(m.layout.sidebar) && m.sidebar.scrollable
	if m.focus == uiFocusSidebar || (overSidebar && m.composerFocusOnly()) {
		return m.sidebar.wheel(int(msg.DeltaY))
	}
	if msg.DeltaX != 0 {
		m.chat.ScrollSelectedShellHorizontal(int(msg.DeltaX))
	}
	lines := int(msg.DeltaY)
	if lines == 0 {
		return nil
	}
	m.frame.markScrollOnly()
	m.applyChatScroll(lines)
	return nil
}

// routePaste gives a paste to a focused inline editor that takes pastes,
// even under a dialog, and otherwise to the dialogs or the editor.
func (m *UI) routePaste(msg tea.PasteMsg) (tea.Cmd, updateFlow) {
	if m.activeInline != nil && m.focus == uiFocusEditor {
		if p, ok := m.activeInline.(dialog.PasteableEditor); ok {
			return p.HandlePaste(msg), flowReturn
		}
	}
	return m.handlePasteMsg(msg), flowContinue
}
