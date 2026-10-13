// Package kit holds layout and rendering helpers shared by the dialog
// package and its subpackages.
package kit

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
)

// Dialog sizing constants.
const (
	// DefaultDialogMaxWidth is the maximum width for standard dialogs.
	DefaultDialogMaxWidth = 70
	// DefaultDialogHeight is the default height for standard dialogs.
	DefaultDialogHeight = 20
	// TitleContentHeight is the height of the title content line.
	TitleContentHeight = 1
	// InputContentHeight is the height of the input content line.
	InputContentHeight = 1
)

// DialogInputTextWidth returns the text-area width for a dialog input so
// that the input frame, its prompt (e.g. "> "), the text, and a trailing
// cursor cell all fit within contentWidth. The prompt is rendered outside
// the text area, so it must be subtracted or long values wrap past the
// dialog border.
func DialogInputTextWidth(t *styles.Styles, input textinput.Model, contentWidth int) int {
	const cursorPadding = 1
	return max(0, contentWidth-
		t.Dialog.InputPrompt.GetHorizontalFrameSize()-
		lipgloss.Width(input.Prompt)-
		cursorPadding)
}

// Sizer is satisfied by any list type that can report its total content
// height and accept a viewport size. Both *list.List and *list.FilterableList
// (and wrappers embedding them) implement this.
type Sizer interface {
	TotalHeight() int
	SetSize(width, height int)
}

// SizeDialogList computes the list dimensions within a dialog and calls
// l.SetSize. It accounts for the title, input, help, and view frame sizes
// so callers don't have to repeat the arithmetic. The scrollbar column is
// reserved only when content overflows the viewport.
//
// Returns listHeight, listTotalHeight, and listWidth for callers that need
// them (e.g. to pass to JoinScrollbar or ApplyInfoColumnVisibility).
//
// Parameters:
//   - t: styles for frame/border measurements.
//   - l: the list to size.
//   - innerWidth: dialog content width (total minus View horizontal frame).
//   - dialogHeight: total dialog content height (already clamped).
func SizeDialogList(t *styles.Styles, l Sizer, innerWidth, dialogHeight int) (listHeight, listTotalHeight, listWidth int) {
	heightOffset := t.Dialog.Title.GetVerticalFrameSize() + TitleContentHeight +
		t.Dialog.InputPrompt.GetVerticalFrameSize() + InputContentHeight +
		t.Dialog.HelpView.GetVerticalFrameSize() +
		t.Dialog.View.GetVerticalFrameSize()

	listHeight = max(0, dialogHeight-heightOffset)
	listTotalHeight = l.TotalHeight()

	// Reserve one column for the scrollbar only when it will actually
	// show, so the list otherwise spans the full content width.
	scrollbarWidth := 0
	if listTotalHeight > listHeight {
		scrollbarWidth = 1
	}
	listWidth = max(0, innerWidth-scrollbarWidth)
	l.SetSize(listWidth, listHeight)
	return listHeight, listTotalHeight, listWidth
}

// JoinScrollbar appends a vertical scrollbar to the right of view when the
// content overflows its viewport, and returns view unchanged otherwise.
// contentSize is the total content height, viewportSize the visible height,
// and offset the current scroll position.
func JoinScrollbar(t *styles.Styles, view string, height, contentSize, viewportSize, offset int) string {
	if sb := common.Scrollbar(t, height, contentSize, viewportSize, offset); sb != "" {
		return lipgloss.JoinHorizontal(lipgloss.Top, view, sb)
	}
	return view
}

// AdjustOnboardingInputCursor removes the dialog view frame offset from an
// input cursor. Onboarding dialogs render without Dialog.View frame, while
// InputCursor includes that frame offset for regular dialogs.
func AdjustOnboardingInputCursor(t *styles.Styles, cur *tea.Cursor) *tea.Cursor {
	if cur == nil {
		return nil
	}

	dialogStyle := t.Dialog.View
	cur.X -= dialogStyle.GetBorderLeftSize() +
		dialogStyle.GetPaddingLeft() +
		dialogStyle.GetMarginLeft()
	cur.Y -= dialogStyle.GetBorderTopSize() +
		dialogStyle.GetPaddingTop() +
		dialogStyle.GetMarginTop()
	return cur
}

// RenameCursorOffset adjusts a cursor returned by an inline list item's
// text input so it lands at the correct screen position inside a dialog
// that is showing a rename prompt. The math accounts for the dialog
// border/padding/margin, the input prompt frame, and the height of the
// confirmation message rendered above the list. visibleStart and
// visibleEnd are the list's VisibleItemIndices; selectedIndex is the
// list's Selected index. The cursor Y is advanced one row per item
// between visibleStart and selectedIndex.
func RenameCursorOffset(
	t *styles.Styles,
	cur *tea.Cursor,
	messageHeight int,
	visibleStart, visibleEnd, selectedIndex int,
) *tea.Cursor {
	if cur == nil {
		return nil
	}
	titleStyle := t.Dialog.Sessions.RenamingingTitle
	dialogStyle := t.Dialog.Sessions.RenamingView
	inputStyle := t.Dialog.InputPrompt

	cur.X += inputStyle.GetBorderLeftSize() +
		inputStyle.GetMarginLeft() +
		inputStyle.GetPaddingLeft() +
		dialogStyle.GetBorderLeftSize() +
		dialogStyle.GetPaddingLeft() +
		dialogStyle.GetMarginLeft()
	cur.Y += titleStyle.GetVerticalFrameSize() +
		inputStyle.GetBorderTopSize() +
		inputStyle.GetMarginTop() +
		inputStyle.GetPaddingTop() +
		inputStyle.GetBorderBottomSize() +
		inputStyle.GetMarginBottom() +
		inputStyle.GetPaddingBottom() +
		dialogStyle.GetPaddingTop() +
		dialogStyle.GetBorderTopSize() +
		messageHeight - 1

	for i := visibleStart; i <= visibleEnd && i != selectedIndex && selectedIndex > -1; i++ {
		cur.Y++
	}
	return cur
}
