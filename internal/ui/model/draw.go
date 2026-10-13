package model

import (
	"image"
	"math/rand"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/home"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/logo"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/crush/internal/version"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/ultraviolet/screen"
	"github.com/charmbracelet/x/ansi"
)

// drawHeader draws the header section of the UI.
func (m *UI) drawHeader(scr uv.Screen, area uv.Rectangle) {
	m.header.drawHeader(
		scr,
		area,
		m.session,
		m.isCompact,
		m.detailsOpen,
		area.Dx(),
		m.lspErrorCount(),
		m.hyperCredits,
		m.logoFrame,
		m.gitBranch,
	)
}

// Draw implements [uv.Drawable] and draws the UI model.
func (m *UI) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	if m.subagentView != nil && m.activeInline == nil {
		return m.drawSubagentView(scr, area)
	}
	layout := m.generateLayout(area.Dx(), area.Dy())

	if m.layout != layout {
		m.layout = layout
		m.updateSize()
	} else if m.state == uiChat && m.hasSession() {
		// Re-render pills on every draw so the box appears even when
		// the layout footprint hasn't changed (e.g. todos arrived
		// while the panel was collapsed). updateSize already calls
		// renderPills, but only when the layout actually differs;
		// this catches the steady-state case.
		m.renderPills()
	}

	if m.state == uiChat && m.hasSession() && !m.isCompact {
		m.updateSidebarScrollState()
	}

	// Clear the screen first
	screen.Clear(scr)

	switch m.state {
	case uiOnboarding:
		m.drawHeader(scr, layout.header)

		// NOTE: Onboarding flow will be rendered as dialogs below, but
		// positioned at the bottom left of the screen.

	case uiInitialize:
		m.drawHeader(scr, layout.header)

		main := uv.NewStyledString(m.initializeView())
		main.Draw(scr, layout.main)

	case uiLanding:
		m.drawHeader(scr, layout.header)
		if m.sessionSwitchPending() {
			m.drawSessionLoading(scr, layout.main)
		} else {
			main := uv.NewStyledString(m.landingView())
			main.Draw(scr, layout.main)
		}

		if m.activeInline != nil {
			m.activeInline.SetFocused(m.focus == uiFocusEditor)
			if collapsed, ok := m.collapsedInlineEditor(); ok {
				collapsed.DrawCollapsed(scr, layout.editor)
				m.inlineCursor = nil
			} else {
				m.inlineCursor = m.activeInline.Draw(scr, layout.editor)
			}
		} else {
			editor := uv.NewStyledString(m.renderEditorView(scr.Bounds().Dx()))
			editor.Draw(scr, layout.editor)
			m.inlineCursor = nil
		}

	case uiChat:
		if !m.isCompact {
			m.drawSidebar(scr, layout.sidebar)
		}

		if m.sessionSwitchPending() {
			m.drawSessionLoading(scr, layout.main)
		} else {
			m.chat.Draw(scr, layout.main)
		}
		if layout.pills.Dy() > 0 && m.pillsView != "" {
			uv.NewStyledString(m.pillsView).Draw(scr, layout.pills)
		}

		if m.activeInline != nil {
			m.activeInline.SetFocused(m.focus == uiFocusEditor)
			if collapsed, ok := m.collapsedInlineEditor(); ok {
				collapsed.DrawCollapsed(scr, layout.editor)
				m.inlineCursor = nil
			} else {
				m.inlineCursor = m.activeInline.Draw(scr, layout.editor)
			}
		} else {
			editorWidth := scr.Bounds().Dx()
			if !m.isCompact {
				editorWidth -= layout.sidebar.Dx()
			}
			editor := uv.NewStyledString(m.renderEditorView(editorWidth))
			editor.Draw(scr, layout.editor)
			m.inlineCursor = nil
		}
		if layout.usage.Dy() > 0 {
			uv.NewStyledString(m.compactUsage(layout.usage.Dx())).Draw(scr, layout.usage)
		}
		if layout.tasks.Dy() > 0 {
			uv.NewStyledString(m.renderTasks(layout.tasks.Dx())).Draw(scr, layout.tasks)
		}

		// Draw details overlay in compact mode when open
		if m.isCompact && m.detailsOpen {
			m.drawSessionDetails(scr, layout.sessionDetails)
		}
	}

	isOnboarding := m.state == uiOnboarding

	// Add status and help layer
	m.status.SetHideHelp(isOnboarding)
	m.status.SetMode(m.mode, m.yoloModeCached())
	m.status.Draw(scr, layout.status)

	// Draw completions popup if open
	if !isOnboarding {
		m.mention.draw(scr, area.Dx())
	}

	// Debugging rendering (visually see when the tui rerenders)
	if os.Getenv("CRUSH_UI_DEBUG") == "true" {
		debugView := lipgloss.NewStyle().Background(lipgloss.ANSIColor(rand.Intn(256))).Width(4).Height(2)
		debug := uv.NewStyledString(debugView.String())
		debug.Draw(scr, image.Rectangle{
			Min: image.Pt(4, 1),
			Max: image.Pt(8, 3),
		})
	}

	// This needs to come last to overlay on top of everything. We always pass
	// the full screen bounds because the dialogs will position themselves
	// accordingly.
	if m.dialog.HasDialogs() {
		return m.dialog.Draw(scr, scr.Bounds())
	}

	switch m.focus {
	case uiFocusEditor:
		if m.layout.editor.Dy() <= 0 {
			// Don't show cursor if editor is not visible
			return nil
		}
		if m.detailsOpen && m.isCompact {
			// Don't show cursor if details overlay is open
			return nil
		}

		if m.activeInline != nil {
			if cur := m.inlineCursor; cur != nil {
				cur.X += m.layout.editor.Min.X // Inline editor draws from area left
				cur.Y += m.layout.editor.Min.Y // and top
				return cur
			}
			return nil
		}

		if m.textarea.Focused() {
			cur := m.textarea.Cursor()
			cur.X += m.layout.editor.Min.X                 // The composer's left edge
			cur.Y += m.layout.editor.Min.Y + editorTextTop // Separator and attachments rows
			return cur
		}
	}
	return nil
}

// mouseMode determines the Bubble Tea mouse reporting mode to request for
// the current frame. When mouse support is disabled via configuration, no
// mouse mode is requested so the terminal emulator (or tmux) can handle
// text selection, copy/paste, and scrolling natively. Inline editors and
// hoverable dialogs need motion events even without a button pressed (e.g.
// for hover/drag), so they use MouseModeAllMotion; everything else only
// needs click/drag tracking via MouseModeCellMotion.
func mouseMode(enabled, wantsMotion bool) tea.MouseMode {
	switch {
	case !enabled:
		return tea.MouseModeNone
	case wantsMotion:
		return tea.MouseModeAllMotion
	default:
		return tea.MouseModeCellMotion
	}
}

// View renders the UI model's view. A background check changes nothing on
// screen, so it reuses the last frame instead of drawing one.
func (m *UI) View() tea.View {
	if v, ok := m.frame.quietView(); ok {
		return v
	}
	return m.frame.remember(m.view())
}

func (m *UI) view() tea.View {
	var v tea.View
	v.AltScreen = true
	v.KeyboardEnhancements.ReportEventTypes = true
	m.watchBackground.Store(!m.isTransparent)
	if !m.isTransparent {
		v.BackgroundColor = m.viewBackground()
	}
	v.MouseMode = mouseMode(m.mouseEnabled, m.activeInline != nil || m.dialog.HandlesHover())
	v.ReportFocus = m.caps.ReportFocusEvents
	v.WindowTitle = "crush " + home.Short(m.com.Workspace.WorkingDir())

	key, cacheable := m.currentFrameKey()
	if cacheable {
		if content, cursor, ok := m.frames.get(key); ok {
			v.Content = content
			v.Cursor = cursor
			m.applyProgressBar(&v)
			v.ProgramStatus = m.programStatus()
			return v
		}
	}

	canvas := uv.NewScreenBuffer(m.width, m.height)
	canvas.Method = m.caps.WidthMethod()
	v.Cursor = m.Draw(canvas, canvas.Bounds())

	content := strings.ReplaceAll(canvas.Render(), "\r\n", "\n") // normalize newlines
	contentLines := strings.Split(content, "\n")
	for i, line := range contentLines {
		// Trim trailing spaces for concise rendering
		contentLines[i] = strings.TrimRight(line, " ")
	}

	content = strings.Join(contentLines, "\n")

	v.Content = content
	if cacheable {
		m.storeFrame(key, content, v.Cursor)
	}
	m.applyProgressBar(&v)
	v.ProgramStatus = m.programStatus()

	return v
}

// applyProgressBar attaches the terminal progress bar while the agent is
// busy. Kept outside the frame cache so the randomized value stays fresh.
func (m *UI) applyProgressBar(v *tea.View) {
	if m.progressBarEnabled && m.sendProgressBar && m.isAgentBusy() {
		// HACK: use a random percentage to prevent ghostty from hiding it
		// after a timeout.
		v.ProgressBar = tea.NewProgressBar(tea.ProgressBarIndeterminate, rand.Intn(100))
	}
}

// renderEditorView renders the attachments row (blank without attachments,
// keeping the composer apart from the chat), then the editor on its band:
// a row of padding, the text, a row of padding, then the margin before the
// status line. Every band row is filled to the text's full width.
func (m *UI) renderEditorView(width int) string {
	var attachmentsView string
	if m.attachments.Count() > 0 {
		attachmentsView = m.attachments.Render(width)
	}
	rows := strings.Split(m.textarea.View(), "\n")
	bandWidth := 0
	for _, row := range rows {
		bandWidth = max(bandWidth, ansi.StringWidth(row))
	}
	bg := m.com.Styles.Editor.Textarea.Focused.Base.GetBackground()
	if !m.textarea.Focused() {
		bg = m.com.Styles.Editor.Textarea.Blurred.Base.GetBackground()
	}
	band := make([]string, 0, len(rows)+3)
	band = append(band, attachmentsView, common.OnBand("", bandWidth, bg))
	for _, row := range rows {
		band = append(band, common.OnBand(row, bandWidth, bg))
	}
	band = append(band, common.OnBand("", bandWidth, bg))
	return strings.Join(append(band, ""), "\n") // margin at bottom of editor
}

// cacheSidebarLogo renders and caches the sidebar logo at the specified width.
func (m *UI) cacheSidebarLogo(width int) {
	m.sidebar.logo = renderLogo(m.com.Styles, true, m.com.IsHyper(), width, m.logoFrame)
}

// drawSessionDetails draws the session details in compact mode.
func (m *UI) drawSessionDetails(scr uv.Screen, area uv.Rectangle) {
	if m.session == nil {
		return
	}

	s := m.com.Styles

	width := area.Dx() - s.CompactDetails.View.GetHorizontalFrameSize()
	height := area.Dy() - s.CompactDetails.View.GetVerticalFrameSize()

	title := s.CompactDetails.Title.Width(width).MaxHeight(2).Render(m.session.Title)
	blocks := []string{
		title,
		"",
		m.modelInfo(width),
		"",
	}

	detailsHeader := lipgloss.JoinVertical(
		lipgloss.Left,
		blocks...,
	)

	version := s.CompactDetails.Version.Width(width).AlignHorizontal(lipgloss.Right).Render(version.Version)

	remainingHeight := height - lipgloss.Height(detailsHeader) - lipgloss.Height(version)

	const maxSectionWidth = 50
	sectionWidth := max(1, min(maxSectionWidth, width/4-2)) // account for spacing between sections
	maxItemsPerSection := remainingHeight - 3               // Account for section title and spacing

	lspSection := m.lspInfo(sectionWidth, maxItemsPerSection, false)
	mcpSection := m.mcpInfo(sectionWidth, maxItemsPerSection, false)
	skillsSection := m.skillsInfo(sectionWidth, maxItemsPerSection, false)
	filesSection := m.filesInfo(m.com.Workspace.WorkingDir(), sectionWidth, maxItemsPerSection, false)
	sections := lipgloss.JoinHorizontal(lipgloss.Top, filesSection, " ", lspSection, " ", mcpSection, " ", skillsSection)
	uv.NewStyledString(
		s.CompactDetails.View.
			Width(area.Dx()).
			Render(
				lipgloss.JoinVertical(
					lipgloss.Left,
					detailsHeader,
					sections,
					version,
				),
			),
	).Draw(scr, area)
}

// renderLogo renders the Crush logo with the given styles and dimensions,
// at the given animation frame.
func renderLogo(t *styles.Styles, compact, hyper bool, width int, frame logoFrame) string {
	return logo.Render(t.Logo.GradCanvas, version.Version, compact, logo.Opts{
		Flow:         frame.flow,
		FlowColor:    t.Logo.FlowColor,
		Shine:        frame.shine,
		ShineColor:   t.Logo.ShineColor,
		TitleColorA:  t.Logo.TitleColorA,
		TitleColorB:  t.Logo.TitleColorB,
		CharmColor:   t.Logo.CharmColor,
		VersionColor: t.Logo.VersionColor,
		Width:        width,
		Hyper:        hyper,
	})
}
