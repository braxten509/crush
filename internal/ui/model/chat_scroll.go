package model

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/ui/list"
)

// scrollbarHideMsg is sent to hide the scrollbar after the timeout period.
// chat is the chat that armed it, so a second chat (the sub-agent view)
// hides its own scrollbar; nil means the main chat.
type scrollbarHideMsg struct {
	seq  int // sequence number to ignore stale messages
	chat *Chat
}

// sidebarScrollbarHideMsg is sent to hide the sidebar scrollbar after timeout.
type sidebarScrollbarHideMsg struct {
	seq int
}

// scrollbarHideCmd returns a command that sends chat a scrollbarHideMsg
// after the timeout.
func scrollbarHideCmd(chat *Chat, seq int) tea.Cmd {
	return tea.Tick(scrollbarHideDuration, func(_ time.Time) tea.Msg {
		return scrollbarHideMsg{seq: seq, chat: chat}
	})
}

// sidebarScrollbarHideCmd returns a command that sends a sidebarScrollbarHideMsg
// after the timeout.
func sidebarScrollbarHideCmd(seq int) tea.Cmd {
	return tea.Tick(scrollbarHideDuration, func(_ time.Time) tea.Msg {
		return sidebarScrollbarHideMsg{seq: seq}
	})
}

// scrollbarHideDuration is how long the scrollbar remains visible after scroll activity.
const scrollbarHideDuration = 2 * time.Second

// AtBottom returns whether the chat list is currently scrolled to the bottom.
func (m *Chat) AtBottom() bool {
	return m.list.AtBottom()
}

// Follow returns whether the chat view is in follow mode (auto-scroll to
// bottom on new messages).
func (m *Chat) Follow() bool {
	return m.follow
}

// ScrollToBottom scrolls the chat view to the bottom.
// Does not trigger scrollbar visibility (auto-scroll to show new content).
func (m *Chat) ScrollToBottom() {
	m.list.ScrollToBottom()
	m.follow = true
}

// ScrollToTop scrolls the chat view to the top.
func (m *Chat) ScrollToTop() {
	m.list.ScrollToTop()
	m.follow = false // Disable follow mode when user scrolls up
	m.showScrollbar()
}

// ScrollBy scrolls the chat view by the given number of line deltas.
func (m *Chat) ScrollBy(lines int) {
	m.list.ScrollBy(lines)
	if lines < 0 {
		// Scrolling up always disables follow mode.
		m.follow = false
	} else if m.AtBottom() {
		// Scrolling down re-enables follow when we reach the bottom.
		m.follow = true
	}
	m.showScrollbar()
}

// ScrollToSelected scrolls the chat view to the selected item.
func (m *Chat) ScrollToSelected() {
	m.list.ScrollToSelected()
	m.follow = m.AtBottom() // Disable follow mode if user scrolls up
	m.showScrollbar()
}

// ScrollToIndex scrolls the chat view to the item at the given index.
func (m *Chat) ScrollToIndex(index int) {
	m.list.ScrollToIndex(index)
	m.follow = m.AtBottom() // Disable follow mode if user scrolls up
	m.showScrollbar()
}

// showScrollbar reveals the scrollbar and restarts its auto-hide timeout.
// The tick itself is scheduled by EnsureScrollbarHide.
func (m *Chat) showScrollbar() {
	// Only start timer for "default" mode
	if m.scrollbarMode != config.ScrollbarDefault {
		return
	}
	m.scrollbarVisible = true
	m.scrollbarHideSeq++
}

// EnsureScrollbarHide schedules the hide tick for a visible scrollbar whose
// current timeout has none outstanding. Each scroll bumps the sequence, so a
// fresh deadline is armed for the newest activity while earlier ticks go stale
// and get ignored. This is the sole place the timeout is armed, mirroring the
// shared animation clock, so no call site can drop it.
func (m *Chat) EnsureScrollbarHide() tea.Cmd {
	if m.scrollbarMode != config.ScrollbarDefault || !m.scrollbarVisible {
		return nil
	}
	if m.scrollbarHideArmed == m.scrollbarHideSeq {
		return nil
	}
	m.scrollbarHideArmed = m.scrollbarHideSeq
	return scrollbarHideCmd(m, m.scrollbarHideSeq)
}

// HideScrollbar hides the scrollbar if the sequence matches. It stays up
// while being dragged: the drag counts as fresh activity, so the next
// update arms another timeout.
func (m *Chat) HideScrollbar(seq int) {
	// Only hide scrollbar for "default" mode
	if m.scrollbarMode != config.ScrollbarDefault || seq != m.scrollbarHideSeq {
		return
	}
	if m.DraggingScrollbar() {
		m.showScrollbar()
		return
	}
	m.scrollbarVisible = false
	m.scrollbarHideArmed = 0
}

// resetScrollbar hides the scrollbar and forgets any outstanding timeout, for
// when the list stops showing the content it was scrolled through.
func (m *Chat) resetScrollbar() {
	m.scrollbarVisible = false
	m.scrollbarHideArmed = 0
}

// ScrollToBottomAndSelectLast scrolls the chat view to the bottom and selects
// the last item.
func (m *Chat) ScrollToBottomAndSelectLast() {
	m.ScrollToBottom()
	m.SelectLast()
}

// SelectedItemInView returns whether the selected item is currently in view.
func (m *Chat) SelectedItemInView() bool {
	return m.list.SelectedItemInView()
}

func (m *Chat) isSelectable(index int) bool {
	item := m.list.ItemAt(index)
	if item == nil {
		return false
	}
	_, ok := item.(list.Focusable)
	return ok
}

// SetSelected sets the selected message index in the chat list.
func (m *Chat) SetSelected(index int) {
	m.list.SetSelected(index)
	if index < 0 || index >= m.list.Len() {
		return
	}
	for {
		if m.isSelectable(m.list.Selected()) {
			return
		}
		if m.list.SelectNext() {
			continue
		}
		// If we're at the end and the last item isn't selectable, walk backwards
		// to find the nearest selectable item.
		for {
			if !m.list.SelectPrev() {
				return
			}
			if m.isSelectable(m.list.Selected()) {
				return
			}
		}
	}
}

// SelectPrev selects the previous message in the chat list.
func (m *Chat) SelectPrev() {
	for {
		if !m.list.SelectPrev() {
			return
		}
		if m.isSelectable(m.list.Selected()) {
			return
		}
	}
}

// SelectNext selects the next message in the chat list.
func (m *Chat) SelectNext() {
	for {
		if !m.list.SelectNext() {
			return
		}
		if m.isSelectable(m.list.Selected()) {
			return
		}
	}
}

// SelectFirst selects the first message in the chat list.
func (m *Chat) SelectFirst() {
	if !m.list.SelectFirst() {
		return
	}
	if m.isSelectable(m.list.Selected()) {
		return
	}
	for {
		if !m.list.SelectNext() {
			return
		}
		if m.isSelectable(m.list.Selected()) {
			return
		}
	}
}

// SelectLast selects the last message in the chat list.
func (m *Chat) SelectLast() {
	if !m.list.SelectLast() {
		return
	}
	if m.isSelectable(m.list.Selected()) {
		return
	}
	for {
		if !m.list.SelectPrev() {
			return
		}
		if m.isSelectable(m.list.Selected()) {
			return
		}
	}
}

// SelectFirstInView selects the first message currently in view.
func (m *Chat) SelectFirstInView() {
	startIdx, endIdx := m.list.VisibleItemIndices()
	for i := startIdx; i <= endIdx; i++ {
		if m.isSelectable(i) {
			m.list.SetSelected(i)
			return
		}
	}
}

// SelectLastInView selects the last message currently in view.
func (m *Chat) SelectLastInView() {
	startIdx, endIdx := m.list.VisibleItemIndices()
	for i := endIdx; i >= startIdx; i-- {
		if m.isSelectable(i) {
			m.list.SetSelected(i)
			return
		}
	}
}

// SelectNearestInView moves an out-of-view selection to the visible edge
// nearest to it: the top row when the selection is above the viewport, the
// bottom row when it is below. With no selection, scrolledUp picks the
// bottom row (the content the user is moving towards) and otherwise the
// top row.
func (m *Chat) SelectNearestInView(scrolledUp bool) {
	startIdx, _ := m.list.VisibleItemIndices()
	sel := m.list.Selected()
	switch {
	case sel < 0:
		if scrolledUp {
			m.SelectLastInView()
		} else {
			m.SelectFirstInView()
		}
	case sel < startIdx:
		m.SelectFirstInView()
	default:
		m.SelectLastInView()
	}
}

// scrollbarThumb returns the thumb's first row and size and the scroll
// range, matching [common.Scrollbar]; ok is false when the chat doesn't
// scroll.
func (m *Chat) scrollbarThumb() (pos, size, track, maxOffset int, ok bool) {
	height := m.list.Height() - 1
	if m.scrollbarMode == config.ScrollbarNever || height <= 0 || !m.list.Overflows(m.list.Height()) {
		return 0, 0, 0, 0, false
	}
	content := m.list.TotalHeight() - 1
	if content <= height {
		return 0, 0, 0, 0, false
	}
	size = max(1, height*height/content)
	track = height - size
	maxOffset = content - height
	if track > 0 {
		pos = min(track, m.list.Offset()*track/maxOffset)
	}
	return pos, size, track, maxOffset, true
}

// HandleScrollbarPress starts dragging the scrollbar when (x, y) is on
// its column, even while it's hidden (the column is the chat's empty
// right edge). Pressing the track jumps the thumb there, held by its
// middle.
func (m *Chat) HandleScrollbarPress(x, y int) bool {
	if x < 0 || x != m.scrollbarEdge {
		return false
	}
	pos, size, _, _, ok := m.scrollbarThumb()
	if !ok || y < 0 || y >= m.list.Height()-1 {
		return false
	}
	m.scrollbarGrab = y - pos
	if y < pos || y >= pos+size {
		m.scrollbarGrab = size / 2
	}
	m.HandleScrollbarDrag(y)
	return true
}

// DraggingScrollbar reports whether a scrollbar drag is in progress.
func (m *Chat) DraggingScrollbar() bool {
	return m.scrollbarGrab >= 0
}

// HandleScrollbarDrag scrolls so the thumb follows the pointer at row y.
func (m *Chat) HandleScrollbarDrag(y int) {
	if m.scrollbarGrab < 0 {
		return
	}
	_, _, track, maxOffset, ok := m.scrollbarThumb()
	if !ok {
		return
	}
	target := maxOffset
	if track > 0 {
		pos := min(max(y-m.scrollbarGrab, 0), track)
		target = (pos*maxOffset + track/2) / track
	}
	if delta := target - m.list.Offset(); delta != 0 {
		m.ScrollBy(delta)
		return
	}
	m.showScrollbar()
}

// HandleScrollbarRelease ends a scrollbar drag.
func (m *Chat) HandleScrollbarRelease() bool {
	dragging := m.scrollbarGrab >= 0
	m.scrollbarGrab = -1
	return dragging
}
