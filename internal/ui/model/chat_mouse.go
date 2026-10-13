package model

import (
	"strings"
	"time"

	uv "github.com/charmbracelet/ultraviolet"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/charmbracelet/crush/internal/ui/list"
	"github.com/charmbracelet/x/ansi"
	"github.com/clipperhouse/displaywidth"
	"github.com/clipperhouse/uax29/v2/words"
)

// Constants for multi-click detection.
const (
	doubleClickThreshold = 400 * time.Millisecond // 0.4s is typical double-click threshold
	clickTolerance       = 2                      // x,y tolerance for double/tripple click
)

// DelayedClickMsg is sent after the double-click threshold to trigger a
// single-click action (like expansion) if no double-click occurred.
type DelayedClickMsg struct {
	ClickID int
	ItemIdx int
	X, Y    int
}

// HandleMouseDown handles mouse down events for the chat component.
// It detects single, double, and triple clicks for text selection.
// Returns whether the click was handled and an optional command for delayed
// single-click actions.
func (m *Chat) HandleMouseDown(x, y int) (bool, tea.Cmd) {
	if m.list.Len() == 0 {
		return false, nil
	}

	itemIdx, itemY := m.list.ItemIndexAtPosition(x, y)
	if itemIdx < 0 {
		return false, nil
	}
	if !m.isSelectable(itemIdx) {
		return false, nil
	}

	m.heldLinkClick = nil
	m.linkDragged = false

	// Increment pending click ID to invalidate any previous pending clicks.
	m.pendingClickID++
	clickID := m.pendingClickID

	// Detect multi-click (double/triple)
	now := time.Now()
	if now.Sub(m.lastClickTime) <= doubleClickThreshold &&
		abs(x-m.lastClickX) <= clickTolerance &&
		abs(y-m.lastClickY) <= clickTolerance {
		m.clickCount++
	} else {
		m.clickCount = 1
	}
	m.lastClickTime = now
	m.lastClickX = x
	m.lastClickY = y

	// Select the item that was clicked
	m.list.SetSelected(itemIdx)

	var cmd tea.Cmd

	switch m.clickCount {
	case 1:
		// Single click - start selection and schedule delayed click action.
		m.mouseDown = true
		m.mouseDownItem = itemIdx
		m.mouseDownX = x
		m.mouseDownY = itemY
		m.mouseDragItem = itemIdx
		m.mouseDragX = x
		m.mouseDragY = itemY

		// Schedule delayed click action (e.g., expansion) after a short delay.
		// If a double-click occurs, the clickID will be invalidated.
		cmd = tea.Tick(doubleClickThreshold, func(t time.Time) tea.Msg {
			return DelayedClickMsg{
				ClickID: clickID,
				ItemIdx: itemIdx,
				X:       x,
				Y:       itemY,
			}
		})
	case 2:
		// Double click - select word (no delayed action)
		m.selectWord(itemIdx, x, itemY)
	case 3:
		// Triple click - select line (no delayed action)
		m.selectLine(itemIdx, itemY)
		m.clickCount = 0 // Reset after triple click
	}

	return true, cmd
}

// TakeChangesRequest returns the tool group whose changes the last click
// asked to review, or nil.
func (m *Chat) TakeChangesRequest() *chat.ToolGroupItem {
	if g, ok := m.list.SelectedItem().(*chat.ToolGroupItem); ok && g.TakeChangesRequest() {
		return g
	}
	return nil
}

// HandleDelayedClick handles a delayed single-click action (like expansion).
// It only executes if the click ID matches (i.e., no double-click occurred)
// and no text selection was made (drag to select).
func (m *Chat) HandleDelayedClick(msg DelayedClickMsg) bool {
	// Ignore if this click was superseded by a newer click (double/triple).
	if msg.ClickID != m.pendingClickID {
		return false
	}

	// Don't expand if user dragged to select text.
	if m.HasHighlight() {
		return false
	}

	// Use the rendered cells, so wrapped labels, Unicode and hidden targets
	// all have exactly the same hit area as the text the user sees.
	if msg.ItemIdx != m.list.Selected() {
		return false
	}
	selectedItem := m.list.SelectedItem()
	if selectedItem == nil {
		return false
	}
	if item, ok := selectedItem.(interface {
		ImageAt(int, int, int) *message.Attachment
	}); ok {
		if attachment := item.ImageAt(msg.X, msg.Y, m.list.Width()); attachment != nil {
			if m.linkDragged {
				return false
			}
			if m.mouseDown {
				m.heldLinkClick = &msg
			} else {
				m.openImage = attachment
			}
			return true
		}
	}
	if destination := m.itemLinkAt(selectedItem, msg.X, msg.Y); destination != "" {
		if m.linkDragged {
			return false
		}
		if m.mouseDown {
			m.heldLinkClick = &msg
		} else {
			m.openLink = destination
		}
		return true
	}

	// Execute the click action (e.g., expansion).
	if clickable, ok := selectedItem.(list.MouseClickable); ok {
		handled := clickable.HandleMouseClick(ansi.MouseButton1, msg.X, msg.Y)
		// Toggle expansion only when the item signalled it handled the
		// click. Items like AssistantMessageItem only report handled when
		// the click is on their expandable region, so this avoids
		// toggling expansion for clicks outside the clickable area.
		if handled {
			if expandable, ok := selectedItem.(chat.Expandable); ok {
				wasFollowing := m.follow
				expandable.ToggleExpanded()
				if wasFollowing {
					m.ScrollToBottom()
				}
			}
		}
		return handled
	}

	return false
}

// HandleMouseUp handles mouse up events for the chat component.
func (m *Chat) HandleMouseUp(x, y int) bool {
	if !m.mouseDown {
		return false
	}

	// A release elsewhere is a selection, even if no motion event arrived.
	idx, itemY := m.list.ItemIndexAtPosition(x, y)
	if idx != m.mouseDownItem || x != m.mouseDownX || itemY != m.mouseDownY {
		m.linkDragged = true
	}
	m.mouseDown = false
	if held := m.heldLinkClick; held != nil {
		m.heldLinkClick = nil
		m.HandleDelayedClick(*held)
	}
	return true
}

// HandleMouseDrag handles mouse drag events for the chat component.
func (m *Chat) HandleMouseDrag(x, y int) bool {
	if !m.mouseDown {
		return false
	}

	if m.list.Len() == 0 {
		return false
	}

	itemIdx, itemY := m.list.ItemIndexAtPosition(x, y)
	if itemIdx < 0 {
		return false
	}

	if itemIdx != m.mouseDownItem || x != m.mouseDownX || itemY != m.mouseDownY {
		m.linkDragged = true
	}
	m.mouseDragItem = itemIdx
	m.mouseDragX = x
	m.mouseDragY = itemY

	return true
}

// HasHighlight returns whether there is currently highlighted content.
func (m *Chat) HasHighlight() bool {
	startItemIdx, startLine, startCol, endItemIdx, endLine, endCol := m.getHighlightRange()
	return startItemIdx >= 0 && endItemIdx >= 0 && (startLine != endLine || startCol != endCol)
}

// HighlightContent returns the currently highlighted content based on the mouse
// selection. It returns an empty string if no content is highlighted.
func (m *Chat) HighlightContent() string {
	startItemIdx, startLine, startCol, endItemIdx, endLine, endCol := m.getHighlightRange()
	if startItemIdx < 0 || endItemIdx < 0 || startLine == endLine && startCol == endCol {
		return ""
	}

	var selections []string
	for i := startItemIdx; i <= endItemIdx; i++ {
		item := m.list.ItemAt(i)
		if hi, ok := item.(list.Highlightable); ok {
			startLine, startCol, endLine, endCol := hi.Highlight()
			listWidth := m.list.Width()
			var rendered string
			if rr, ok := item.(list.RawRenderable); ok {
				rendered = rr.RawRender(listWidth)
			} else {
				rendered = item.Render(listWidth)
			}
			area := uv.Rect(0, 0, listWidth, lipgloss.Height(rendered))
			if source, ok := item.(list.CopySource); ok {
				if text, matched := list.HighlightSource(source.CopySource(), rendered, area, startLine, startCol, endLine, endCol); matched {
					selections = append(selections, text)
					continue
				}
			}
			selections = append(selections, strings.TrimSuffix(list.HighlightContent(
				rendered,
				area,
				startLine,
				startCol,
				endLine,
				endCol,
			), "\n"))
		}
	}

	return strings.Join(selections, strings.Repeat("\n", m.list.Gap()+1))
}

// ClearMouse clears the current mouse interaction state.
func (m *Chat) ClearMouse() {
	m.mouseDown = false
	m.mouseDownItem = -1
	m.mouseDragItem = -1
	m.lastClickTime = time.Time{}
	m.lastClickX = 0
	m.lastClickY = 0
	m.clickCount = 0
	m.pendingClickID++ // Invalidate any pending delayed click
}

// applyHighlightRange applies the current highlight range to the chat items.
func (m *Chat) applyHighlightRange(idx, selectedIdx int, item list.Item) list.Item {
	if hi, ok := item.(list.Highlightable); ok {
		// Apply highlight
		startItemIdx, startLine, startCol, endItemIdx, endLine, endCol := m.getHighlightRange()
		sLine, sCol, eLine, eCol := -1, -1, -1, -1
		if idx >= startItemIdx && idx <= endItemIdx {
			if idx == startItemIdx && idx == endItemIdx {
				// Single item selection
				sLine = startLine
				sCol = startCol
				eLine = endLine
				eCol = endCol
			} else if idx == startItemIdx {
				// First item - from start position to end of item
				sLine = startLine
				sCol = startCol
				eLine = -1
				eCol = -1
			} else if idx == endItemIdx {
				// Last item - from start of item to end position
				sLine = 0
				sCol = 0
				eLine = endLine
				eCol = endCol
			} else {
				// Middle item - fully highlighted
				sLine = 0
				sCol = 0
				eLine = -1
				eCol = -1
			}
		}

		hi.SetHighlight(sLine, sCol, eLine, eCol)
		return hi.(list.Item)
	}

	return item
}

// getHighlightRange returns the current highlight range.
func (m *Chat) getHighlightRange() (startItemIdx, startLine, startCol, endItemIdx, endLine, endCol int) {
	if m.mouseDownItem < 0 {
		return -1, -1, -1, -1, -1, -1
	}

	downItemIdx := m.mouseDownItem
	dragItemIdx := m.mouseDragItem

	// Determine selection direction
	draggingDown := dragItemIdx > downItemIdx ||
		(dragItemIdx == downItemIdx && m.mouseDragY > m.mouseDownY) ||
		(dragItemIdx == downItemIdx && m.mouseDragY == m.mouseDownY && m.mouseDragX >= m.mouseDownX)

	if draggingDown {
		// Normal forward selection
		startItemIdx = downItemIdx
		startLine = m.mouseDownY
		startCol = m.mouseDownX
		endItemIdx = dragItemIdx
		endLine = m.mouseDragY
		endCol = m.mouseDragX
	} else {
		// Backward selection (dragging up)
		startItemIdx = dragItemIdx
		startLine = m.mouseDragY
		startCol = m.mouseDragX
		endItemIdx = downItemIdx
		endLine = m.mouseDownY
		endCol = m.mouseDownX
	}

	return startItemIdx, startLine, startCol, endItemIdx, endLine, endCol
}

// rawTop is how many rendered rows sit above an item's raw content, such
// as a band's top padding row.
func rawTop(item list.Item) int {
	if padded, ok := item.(interface{ RawTop() int }); ok {
		return padded.RawTop()
	}
	return 0
}

// selectWord selects the word at the given position within an item.
func (m *Chat) selectWord(itemIdx, x, itemY int) {
	item := m.list.ItemAt(itemIdx)
	if item == nil {
		return
	}

	// Get the rendered content for this item
	var rendered string
	if rr, ok := item.(list.RawRenderable); ok {
		rendered = rr.RawRender(m.list.Width())
	} else {
		rendered = item.Render(m.list.Width())
	}

	lines := strings.Split(rendered, "\n")
	row := itemY - rawTop(item)
	if row < 0 || row >= len(lines) {
		return
	}

	// Adjust x for the item's left padding (border + padding) to get content column.
	// The mouse x is in viewport space, but we need content space for boundary detection.
	offset := chat.MessageLeftPaddingTotal
	contentX := max(x-offset, 0)

	line := ansi.Strip(lines[row])
	startCol, endCol := findWordBoundaries(line, contentX)
	if startCol == endCol {
		// No word found at position, fallback to single click behavior
		m.mouseDown = true
		m.mouseDownItem = itemIdx
		m.mouseDownX = x
		m.mouseDownY = itemY
		m.mouseDragItem = itemIdx
		m.mouseDragX = x
		m.mouseDragY = itemY
		return
	}

	// Set selection to the word boundaries (convert back to viewport space).
	// Keep mouseDown true so HandleMouseUp triggers the copy.
	m.mouseDown = true
	m.mouseDownItem = itemIdx
	m.mouseDownX = startCol + offset
	m.mouseDownY = itemY
	m.mouseDragItem = itemIdx
	m.mouseDragX = endCol + offset
	m.mouseDragY = itemY
}

// selectLine selects the entire line at the given position within an item.
func (m *Chat) selectLine(itemIdx, itemY int) {
	item := m.list.ItemAt(itemIdx)
	if item == nil {
		return
	}

	// Get the rendered content for this item
	var rendered string
	if rr, ok := item.(list.RawRenderable); ok {
		rendered = rr.RawRender(m.list.Width())
	} else {
		rendered = item.Render(m.list.Width())
	}

	lines := strings.Split(rendered, "\n")
	row := itemY - rawTop(item)
	if row < 0 || row >= len(lines) {
		return
	}

	// Get line length (stripped of ANSI codes) and account for padding.
	// SetHighlight will subtract the offset, so we need to add it here.
	offset := chat.MessageLeftPaddingTotal
	lineLen := ansi.StringWidth(lines[row])

	// Set selection to the entire line.
	// Keep mouseDown true so HandleMouseUp triggers the copy.
	m.mouseDown = true
	m.mouseDownItem = itemIdx
	m.mouseDownX = 0
	m.mouseDownY = itemY
	m.mouseDragItem = itemIdx
	m.mouseDragX = lineLen + offset
	m.mouseDragY = itemY
}

// findWordBoundaries finds the start and end column of the word at the given column.
// Returns (startCol, endCol) where endCol is exclusive.
func findWordBoundaries(line string, col int) (startCol, endCol int) {
	if line == "" || col < 0 {
		return 0, 0
	}

	i := displaywidth.StringGraphemes(line)
	for i.Next() {
	}

	// Segment the line into words using UAX#29.
	lineCol := 0 // tracks the visited column widths
	lastCol := 0 // tracks the start of the current token
	iter := words.FromString(line)
	for iter.Next() {
		token := iter.Value()
		tokenWidth := displaywidth.String(token)

		graphemeStart := lineCol
		graphemeEnd := lineCol + tokenWidth
		lineCol += tokenWidth

		// If clicked before this token, return the previous token boundaries.
		if col < graphemeStart {
			return lastCol, lastCol
		}

		// Update lastCol to the end of this token for next iteration.
		lastCol = graphemeEnd

		// If clicked within this token, return its boundaries.
		if col >= graphemeStart && col < graphemeEnd {
			// If clicked on whitespace, return empty selection.
			if strings.TrimSpace(token) == "" {
				return col, col
			}
			return graphemeStart, graphemeEnd
		}
	}

	return col, col
}

// abs returns the absolute value of an integer.
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// itemLinkAt includes the visible prefix and preserves links spanning rows.
func (m *Chat) itemLinkAt(item list.Item, x, y int) string {
	if x < 0 || x >= m.list.Width() || y < 0 {
		return ""
	}
	rendered := item.Render(m.list.Width())
	lines := strings.Split(rendered, "\n")
	if y >= len(lines) {
		return ""
	}
	buf := uv.NewScreenBuffer(m.list.Width(), len(lines))
	if m.drawCache != nil {
		buf.Method = m.drawCache.method
	}
	uv.NewStyledString(rendered).Draw(buf, buf.Bounds())
	if cell := buf.CellAt(x, y); cell != nil {
		return cell.Link.URL
	}
	return ""
}
