package model

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/crush/internal/ui/util"
)

// switchTheme makes themeName the active theme. The config write runs off
// the Update goroutine; the theme is applied (and the picker closed) once it
// succeeded, and a failed write drops any preview.
func (m *UI) switchTheme(themeName string) tea.Cmd {
	newStyles, err := styles.LoadTheme(themeName)
	if err != nil {
		return util.ReportError(err)
	}
	return m.writeConfigField("options.tui.active_theme", themeName, func(err error) tea.Cmd {
		if err != nil {
			m.revertThemePreview()
			return util.ReportError(err)
		}
		m.applyTheme(newStyles)
		m.preThemeStyles = nil
		m.userThemeSelected = true
		m.dialog.CloseDialog(dialog.ThemeID)
		return util.ReportInfo("Theme switched to " + themeName)
	})
}

// revertThemePreview restores the styles saved when a theme preview
// started, if one is showing.
func (m *UI) revertThemePreview() {
	if m.preThemeStyles != nil {
		m.applyTheme(*m.preThemeStyles)
		m.preThemeStyles = nil
	}
}
