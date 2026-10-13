package model

import (
	"image"
	"slices"

	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/util"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/ultraviolet/layout"
)

// Compact mode breakpoints.
const (
	compactModeWidthBreakpoint  = 120
	compactModeHeightBreakpoint = 30
)

// Session details panel max height.
const sessionDetailsMaxHeight = 20

// TextareaMaxHeight is the maximum height of the prompt textarea.
const TextareaMaxHeight = 15

// editorHeightMargin is the vertical margin added to the textarea height to
// account for the row that separates the chat from the composer (it holds
// the attachments, right on top of the band), the band's top and bottom
// padding and the bottom margin.
const editorHeightMargin = 4

// editorTextTop is how many rows of the editor area sit above the text: the
// attachments row and the band's top padding.
const editorTextTop = 2

// editorAttachmentsRow is the editor row that shows the attachments, directly
// above the composer band. It is blank without attachments.
const editorAttachmentsRow = 0

// TextareaMinHeight is the minimum height of the prompt textarea: one
// row, between the composer band's padding rows. It grows as lines are typed.
const TextareaMinHeight = 1

// setState changes the UI state and focus.
func (m *UI) setState(state uiState, focus uiFocusState) {
	if state == uiLanding {
		// Always turn off compact mode when going to landing
		m.isCompact = false
	}
	m.state = state
	// Callers bring the textarea and the chat in line themselves.
	m.setFocus(focus, focusRecordOnly)
	// Changing the state may change layout, so update it.
	m.updateLayoutAndSize()
}

// toggleCompactMode toggles compact mode between uiChat and uiChatCompact states.
func (m *UI) toggleCompactMode() tea.Cmd {
	m.forceCompactMode = !m.forceCompactMode

	err := m.com.Workspace.SetCompactMode(config.ScopeGlobal, m.forceCompactMode)
	if err != nil {
		return util.ReportError(err)
	}

	var cmds []tea.Cmd
	if m.forceCompactMode && m.focus == uiFocusSidebar {
		// The sidebar is going away, so focus the editor again to keep key
		// events routed somewhere useful.
		m.sidebar.scrollbarVisible = false
		if m.activeInline != nil {
			m.focusActiveInline(uiFocusEditor)
		} else {
			cmds = append(cmds, m.setFocus(uiFocusEditor, keepChatFocus))
		}
	}

	m.updateLayoutAndSize()

	return tea.Batch(cmds...)
}

// updateLayoutAndSize updates the layout and sizes of UI components.
func (m *UI) updateLayoutAndSize() {
	// Determine if we should be in compact mode
	if m.state == uiChat {
		if m.forceCompactMode {
			m.isCompact = true
		} else if m.width < compactModeWidthBreakpoint || m.height < compactModeHeightBreakpoint {
			m.isCompact = true
		} else {
			m.isCompact = false
		}
	}

	// First pass sizes components from their current heights.
	previousInlineHeight := -1
	if m.activeInline != nil {
		previousInlineHeight = m.activeInline.Height(m.editorContentWidth())
	}
	m.layout = m.generateLayout(m.width, m.height)
	prevHeight := m.textarea.Height()
	m.updateSize()

	// SetWidth can change textarea or inline-editor height due to soft-wrap
	// recalculation. If that happens, reconcile once with the new height.
	inlineHeightChanged := m.activeInline != nil &&
		m.activeInline.Height(m.editorContentWidth()) != previousInlineHeight
	if m.textarea.Height() != prevHeight || inlineHeightChanged {
		m.layout = m.generateLayout(m.width, m.height)
		m.updateSize()
	}
}

// handleTextareaHeightChange checks whether the textarea height changed and,
// if so, recalculates the layout. When the chat is in follow mode it keeps
// the view scrolled to the bottom. The returned command, if non-nil, must be
// batched by the caller.
func (m *UI) handleTextareaHeightChange(prevHeight int) tea.Cmd {
	if m.textarea.Height() == prevHeight {
		return nil
	}
	m.updateLayoutAndSize()
	if m.state == uiChat && m.chat.Follow() {
		m.chat.ScrollToBottom()
	}
	return nil
}

// updateTextarea updates the textarea for msg and then reconciles layout if
// the textarea height changed as a result.
func (m *UI) updateTextarea(msg tea.Msg) tea.Cmd {
	return m.updateTextareaWithPrevHeight(msg, m.textarea.Height())
}

// updateTextareaWithPrevHeight is for cases when the height of the layout may
// have changed.
//
// Particularly, it's for cases where the textarea changes before
// textarea.Update is called (for example, SetValue, Reset, and InsertRune). We
// pass the height from before those changes took place so we can compare
// "before" vs "after" sizing and recalculate the layout if the textarea grew
// or shrank.
func (m *UI) updateTextareaWithPrevHeight(msg tea.Msg, prevHeight int) tea.Cmd {
	ta, cmd := m.textarea.Update(msg)
	m.textarea = ta
	return tea.Batch(cmd, m.handleTextareaHeightChange(prevHeight))
}

// updateSize updates the sizes of UI components based on the current layout.
func (m *UI) updateSize() {
	m.frame.invalidate()

	// Set status width
	m.status.SetWidth(m.layout.status.Dx())

	m.chat.SetSize(m.layout.main.Dx(), m.layout.main.Dy())
	m.textarea.MaxHeight = TextareaMaxHeight
	editorWidth := m.layout.editor.Dx()
	if m.state == uiChat && m.chat.ListWidth() > 0 {
		// The composer's band ends where the sent messages' bands do.
		editorWidth = m.chat.ListWidth()
	}
	m.textarea.SetWidth(editorWidth)
	// Notices on the status row span the composer's band.
	m.status.SetBand(m.layout.editor.Min.X-m.layout.status.Min.X, editorWidth)
	if resizable, ok := m.activeInline.(dialog.ResizableInlineEditor); ok {
		resizable.SetWidth(m.layout.editor.Dx())
	}
	m.renderPills()

	// Handle different app states
	switch m.state {
	case uiChat:
		if !m.isCompact {
			m.cacheSidebarLogo(m.layout.sidebar.Dx())
		}
	}
}

// generateLayout calculates the layout rectangles for all UI components based
// on the current UI state and terminal dimensions.
func (m *UI) generateLayout(w, h int) uiLayout {
	// The screen area we're working with
	area := image.Rect(0, 0, w, h)

	// The help height: the short hint row is hidden, so the status bar only
	// takes rows when the full help (ctrl+g) is open. Badges and
	// notifications draw on the bottom margin row instead.
	helpHeight := 0
	// The editor height: textarea height + margin for attachments and bottom spacing.
	// When an inline editor is active, use its height instead.
	editorHeight := m.textarea.Height() + editorHeightMargin
	if m.activeInline != nil {
		// The editor content width depends only on terminal width
		// and layout (not on editor height), so passing the current
		// frame's width to Height() keeps layout in sync with the
		// width Draw will use, preventing flicker during fast resize.
		editorWidth := m.editorContentWidth()
		if collapsed, ok := m.collapsedInlineEditor(); ok {
			editorHeight = collapsed.CollapsedHeight() + 1
		} else {
			editorHeight = m.activeInline.Height(editorWidth)
		}
	}
	// The sidebar width
	sidebarWidth := 32
	// The header height
	const landingHeaderHeight = 4

	var helpKeyMap help.KeyMap = m
	if m.status != nil && m.status.ShowingAll() {
		for _, row := range helpKeyMap.FullHelp() {
			helpHeight = max(helpHeight, len(row))
		}
	}

	// Add app margins
	var appRect, helpRect image.Rectangle
	layout.Vertical(
		layout.Len(area.Dy()-helpHeight),
		layout.Fill(1),
	).Split(area).Assign(&appRect, &helpRect)
	appRect.Min.Y += 1
	appRect.Max.Y -= 1
	helpRect.Min.Y -= 1
	appRect.Min.X += 1
	appRect.Max.X -= 1

	if slices.Contains([]uiState{uiOnboarding, uiInitialize, uiLanding}, m.state) {
		// extra padding on left and right for these states
		appRect.Min.X += 1
		appRect.Max.X -= 1
	}
	if m.state == uiChat {
		// The chat runs edge to edge: marks in the first column, text two
		// columns in, and bands out to the right edge (or the sidebar).
		appRect.Min.X = area.Min.X
		if m.isCompact {
			appRect.Max.X = area.Max.X
		}
	}

	uiLayout := uiLayout{
		area:   area,
		status: helpRect,
	}

	// Handle different app states
	switch m.state {
	case uiOnboarding, uiInitialize:
		// Layout
		//
		// header
		// ------
		// main
		// ------
		// help

		var headerRect, mainRect image.Rectangle
		layout.Vertical(
			layout.Len(landingHeaderHeight),
			layout.Fill(1),
		).Split(appRect).Assign(&headerRect, &mainRect)
		uiLayout.header = headerRect
		uiLayout.main = mainRect

	case uiLanding:
		// Layout
		//
		// header
		// ------
		// main
		// ------
		// editor
		// ------
		// help
		var headerRect, mainRect image.Rectangle
		layout.Vertical(
			layout.Len(landingHeaderHeight),
			layout.Fill(1),
		).Split(appRect).Assign(&headerRect, &mainRect)
		var editorRect image.Rectangle
		layout.Vertical(
			layout.Len(mainRect.Dy()-editorHeight),
			layout.Fill(1),
		).Split(mainRect).Assign(&mainRect, &editorRect)
		// The composer runs edge to edge, as in the chat (but the header and
		// main keep their padding).
		editorRect.Min.X = area.Min.X
		editorRect.Max.X = area.Max.X
		uiLayout.header = headerRect
		uiLayout.main = mainRect
		uiLayout.editor = editorRect

	case uiChat:
		// Background tasks are listed under the editor, so the editor
		// block grows to hold them.
		tasksHeight := m.tasksHeight()
		editorHeight += tasksHeight
		if m.isCompact {
			usageHeight := 0
			if usage := m.compactUsage(appRect.Dx()); usage != "" {
				usageHeight = lipgloss.Height(usage)
			}
			editorHeight += usageHeight
			// Layout
			//
			// main
			// ------
			// editor
			// ------
			// help
			//
			// There is no header bar: ctrl+d opens the session details
			// over the top of the chat.
			mainRect := appRect
			detailsHeight := min(sessionDetailsMaxHeight, area.Dy())
			var sessionDetailsArea image.Rectangle
			layout.Vertical(
				layout.Len(detailsHeight),
				layout.Fill(1),
			).Split(appRect).Assign(&sessionDetailsArea, new(image.Rectangle))
			uiLayout.sessionDetails = sessionDetailsArea
			var editorRect image.Rectangle
			layout.Vertical(
				layout.Len(mainRect.Dy()-editorHeight),
				layout.Fill(1),
			).Split(mainRect).Assign(&mainRect, &editorRect)
			pillsHeight := m.pillsAreaHeight()
			if pillsHeight > 0 {
				pillsHeight = min(pillsHeight, mainRect.Dy())
				var chatRect, pillsRect image.Rectangle
				layout.Vertical(
					layout.Len(mainRect.Dy()-pillsHeight),
					layout.Fill(1),
				).Split(mainRect).Assign(&chatRect, &pillsRect)
				uiLayout.main = chatRect
				uiLayout.pills = pillsRect
			} else {
				uiLayout.main = mainRect
			}
			// Add bottom margin to main
			uiLayout.main.Max.Y -= 1
			uiLayout.editor, uiLayout.tasks = splitTasks(editorRect, tasksHeight)
			uiLayout.usage = uiLayout.editor
			uiLayout.usage.Min.Y = max(uiLayout.editor.Min.Y, uiLayout.editor.Max.Y-usageHeight)
			uiLayout.editor.Max.Y = uiLayout.usage.Min.Y
		} else {
			// Layout
			//
			// ------|---
			// main  |
			// ------| side
			// editor|
			// ----------
			// help

			var mainRect, sideRect image.Rectangle
			layout.Horizontal(
				layout.Len(appRect.Dx()-sidebarWidth),
				layout.Fill(1),
			).Split(appRect).Assign(&mainRect, &sideRect)
			// Add padding left
			sideRect.Min.X += 1
			var editorRect image.Rectangle
			layout.Vertical(
				layout.Len(mainRect.Dy()-editorHeight),
				layout.Fill(1),
			).Split(mainRect).Assign(&mainRect, &editorRect)
			mainRect.Max.X -= 1 // Add padding right
			uiLayout.sidebar = sideRect
			pillsHeight := m.pillsAreaHeight()
			if pillsHeight > 0 {
				pillsHeight = min(pillsHeight, mainRect.Dy())
				var chatRect, pillsRect image.Rectangle
				layout.Vertical(
					layout.Len(mainRect.Dy()-pillsHeight),
					layout.Fill(1),
				).Split(mainRect).Assign(&chatRect, &pillsRect)
				uiLayout.main = chatRect
				uiLayout.pills = pillsRect
			} else {
				uiLayout.main = mainRect
			}
			// Add bottom margin to main
			uiLayout.main.Max.Y -= 1
			uiLayout.editor, uiLayout.tasks = splitTasks(editorRect, tasksHeight)
		}
	}

	return uiLayout
}

// uiLayout defines the positioning of UI elements.
type uiLayout struct {
	// area is the overall available area.
	area uv.Rectangle

	// header is the header shown in special cases
	// e.x when the sidebar is collapsed
	// or when in the landing page
	// or in init/config
	header uv.Rectangle

	// main is the area for the main pane. (e.x chat, configure, landing)
	main uv.Rectangle

	// pills is the area for the pills panel.
	pills uv.Rectangle

	// editor is the area for the editor pane.
	editor uv.Rectangle

	// usage shows subscription percentages below the editor in compact mode.
	usage uv.Rectangle

	// tasks lists background sub-agents under the editor.
	tasks uv.Rectangle

	// sidebar is the area for the sidebar.
	sidebar uv.Rectangle

	// status is the area for the status view.
	status uv.Rectangle

	// session details is the area for the session details overlay in compact mode.
	sessionDetails uv.Rectangle
}

// editorContentWidth returns the content width available to the
// editor area for the current state. It depends only on terminal
// width and layout (not on editor height), so it can be computed
// before the editor's height is known. This is the single source
// of truth for the inline editor width used by both layout sizing
// and Height() queries.
func (m *UI) editorContentWidth() int {
	width := m.width - 2 // appRect horizontal margins
	if m.state == uiChat && !m.isCompact {
		width -= 30 // sidebar column
	}
	return width
}

// collapsedInlineEditor returns the active inline editor when it should use
// its compact representation while chat has focus.
func (m *UI) collapsedInlineEditor() (dialog.CollapsibleInlineEditor, bool) {
	if m.focus == uiFocusEditor {
		return nil, false
	}
	collapsible, ok := m.activeInline.(dialog.CollapsibleInlineEditor)
	if !ok || !collapsible.ShouldCollapse(m.editorContentWidth(), m.height) {
		return nil, false
	}
	return collapsible, true
}
