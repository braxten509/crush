package model

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/crush/internal/ui/util"
)

// defaultThemeName is the theme used when none is configured.
const defaultThemeName = "charmtone-panther"

// themeController tracks which theme is applied and the styles a theme
// preview replaced.
type themeController struct {
	// key identifies the currently applied theme so applyTheme can skip the
	// expensive style rebuild when switching to a provider that resolves
	// to the same theme.
	key string

	// userSelected records that the user explicitly chose a theme during
	// this session. It guards against provider-driven theme swaps
	// discarding that choice, even in client/server mode where the config
	// round-trip may not reflect the selection immediately.
	userSelected bool

	// preview stores the styles from before a theme preview so it can be
	// reverted; nil while no preview is showing.
	preview *styles.Styles
}

// swapForProvider reports whether selecting a model of providerID should
// swap the theme, and records the provider's theme as applied when it
// should. A theme the user chose (now, or in the config) always wins.
func (t *themeController) swapForProvider(providerID string, configured bool) bool {
	if t.userSelected || configured {
		return false
	}
	key := styles.ThemeKeyForProvider(providerID)
	if key == t.key {
		return false
	}
	t.key = key
	return true
}

// beginPreview saves current as the styles to revert to, unless a preview
// already saved them.
func (t *themeController) beginPreview(current *styles.Styles) {
	if t.preview == nil {
		saved := current.Clone()
		t.preview = &saved
	}
}

// endPreview drops the saved styles: what is applied now stays.
func (t *themeController) endPreview() {
	t.preview = nil
}

// takePreview returns the saved styles, if a preview is showing, and ends
// the preview.
func (t *themeController) takePreview() (styles.Styles, bool) {
	if t.preview == nil {
		return styles.Styles{}, false
	}
	saved := *t.preview
	t.preview = nil
	return saved, true
}

// handleThemeAction handles the actions of the theme picker and editor.
// ok is false for any other action.
func (m *UI) handleThemeAction(action dialog.Action) (cmd tea.Cmd, ok bool) {
	switch msg := action.(type) {
	case dialog.ActionSwitchTheme:
		return m.switchTheme(msg.Theme), true
	case dialog.ActionPreviewTheme:
		if newStyles, err := styles.LoadTheme(msg.Theme); err == nil {
			m.showThemePreview(newStyles)
		}
	case dialog.ActionRevertThemePreview:
		m.revertThemePreview()
		m.dialog.CloseDialog(dialog.ThemeID)
	case dialog.ActionPreviewThemePalette:
		if newStyles, err := styles.LoadPaletteTheme(msg.Base, msg.Palette); err == nil {
			m.showThemePreview(newStyles)
		}
	case dialog.ActionSaveThemePalette:
		return m.saveThemePalette(msg), true
	case dialog.ActionEditTheme:
		m.openThemeEditorDialog(msg.Name)
	case dialog.ActionRevertThemePalette:
		m.revertThemePreview()
		m.dialog.CloseDialog(dialog.ThemeEditorID)
	case dialog.ActionRevertOverriddenTheme:
		return m.revertOverriddenTheme(msg.Name), true
	case dialog.ActionCreateTheme:
		return m.createTheme(msg.Base, msg.Name), true
	case dialog.ActionRenameTheme:
		return m.renameTheme(msg.OldName, strings.ToLower(msg.NewName)), true
	case dialog.ActionDeleteTheme:
		return m.deleteTheme(msg.Name), true
	default:
		return nil, false
	}
	return nil, true
}

// showThemePreview applies newStyles as a preview, saving the styles to
// revert to first.
func (m *UI) showThemePreview(newStyles styles.Styles) {
	m.theme.beginPreview(m.com.Styles)
	m.previewTheme(newStyles)
}

// revertThemePreview restores the styles saved when a theme preview
// started, if one is showing.
func (m *UI) revertThemePreview() {
	if saved, ok := m.theme.takePreview(); ok {
		m.applyTheme(saved)
	}
}

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
		m.theme.endPreview()
		m.theme.userSelected = true
		m.dialog.CloseDialog(dialog.ThemeID)
		return util.ReportInfo("Theme switched to " + themeName)
	})
}

// saveThemePalette stores the theme editor's palette and, when the theme is
// the active one, applies it.
func (m *UI) saveThemePalette(msg dialog.ActionSaveThemePalette) tea.Cmd {
	// The theme is stored under its own name; Base only identifies the
	// built-in palette its colors are derived from.
	themeName := msg.Name
	if themeName == "" {
		themeName = msg.Base
	}

	// Write the file before touching the UI so a failed save never
	// leaves colors applied that did not reach disk.
	savePath, err := styles.ThemePath(themeName)
	if err != nil {
		return util.ReportError(err)
	}
	tf := &styles.ThemeFile{Base: msg.Base, Palette: msg.Palette}
	if err := styles.SaveThemeFile(savePath, tf); err != nil {
		return util.ReportError(err)
	}

	// Only take over the whole UI when the saved theme is the active
	// one (or the implicit default). Otherwise the preview backup
	// stays intact so esc restores the user's real theme instead of
	// leaving the edited colors applied until restart.
	activeTheme := common.ThemeNameFromConfig(m.com.Config())
	isActive := strings.EqualFold(activeTheme, themeName)
	if activeTheme == "" {
		isActive = strings.EqualFold(themeName, defaultThemeName)
	}
	if isActive {
		newStyles, err := styles.LoadTheme(themeName)
		if err != nil {
			return util.ReportError(err)
		}
		m.applyTheme(newStyles)
		m.theme.endPreview()
	}
	m.dialog.CloseDialog(dialog.ThemeEditorID)
	if td, ok := m.dialog.Dialog(dialog.ThemeID).(*dialog.Theme); ok {
		td.RefreshThemes(themeName)
	}
	return util.ReportInfo("Theme saved")
}

// revertOverriddenTheme drops any user override layered on top of the
// built-in theme name: the shadowing theme file and the config palette
// entry.
func (m *UI) revertOverriddenTheme(name string) tea.Cmd {
	if path, err := styles.FindThemeFile(name); err == nil {
		if err := os.Remove(path); err != nil {
			return util.ReportError(fmt.Errorf("revert theme: %w", err))
		}
	}
	// If the reverted theme is the active one, re-apply the pristine
	// built-in so the change is visible immediately.
	if strings.EqualFold(common.ThemeNameFromConfig(m.com.Config()), name) {
		if newStyles, err := styles.LoadTheme(name); err == nil {
			m.applyTheme(newStyles)
		}
	}
	m.dialog.CloseDialog(dialog.ThemeID)
	m.openThemeDialog()
	return util.ReportInfo("Reverted " + name + " to its built-in colors")
}

// createTheme saves a new theme named name with the palette of base and
// makes it the active theme. The config write runs off the Update
// goroutine; the editor opens once it succeeded.
func (m *UI) createTheme(base, name string) tea.Cmd {
	if base == "" {
		base = defaultThemeName
	}
	exported, err := styles.ExportResolvedPalette(base)
	if err != nil {
		// Fall back to the default theme when the base theme is no
		// longer resolvable (e.g. a user theme that was since deleted).
		base = defaultThemeName
		exported, err = styles.ExportResolvedPalette(base)
		if err != nil {
			return util.ReportError(err)
		}
	}
	savePath, err := styles.ThemePath(name)
	if err != nil {
		return util.ReportError(err)
	}
	// ExportResolvedPalette already pins Base to the built-in root the
	// palette was resolved from. Keep it so the new theme stays
	// loadable even if a user theme used as the source is later
	// deleted or renamed.
	if err := styles.SaveThemeFile(savePath, exported); err != nil {
		return util.ReportError(err)
	}
	return m.writeConfigField("options.tui.active_theme", name, func(err error) tea.Cmd {
		if err != nil {
			return util.ReportError(err)
		}
		m.dialog.CloseDialog(dialog.ThemeNewID)
		m.dialog.CloseDialog(dialog.ThemeID)
		m.openThemeEditorDialog(name)
		return util.ReportInfo("Created new theme: " + name)
	})
}

// renameTheme renames a user theme. When it is the active theme, the config
// follows off the Update goroutine, and a failed write undoes the rename.
func (m *UI) renameTheme(oldName, newName string) tea.Cmd {
	oldPath, newPath, err := styles.RenameThemeFile(oldName, newName)
	if err != nil {
		return util.ReportError(err)
	}
	renamed := func() tea.Cmd {
		m.dialog.CloseDialog(dialog.ThemeID)
		m.openThemeDialog()
		return util.ReportInfo("Renamed theme " + oldName + " to " + newName)
	}
	cfg := m.com.Config()
	if cfg == nil || cfg.Options == nil || cfg.Options.TUI == nil || !strings.EqualFold(cfg.Options.TUI.ActiveTheme, oldName) {
		return renamed()
	}
	return m.writeConfigField("options.tui.active_theme", newName, func(err error) tea.Cmd {
		if err != nil {
			if rollbackErr := os.Rename(newPath, oldPath); rollbackErr != nil {
				slog.Error("Failed to roll back theme rename", "error", rollbackErr)
			}
			return util.ReportError(err)
		}
		return renamed()
	})
}

// deleteTheme deletes a user theme. When it was the active theme, the
// config falls back to the default theme off the Update goroutine, and the
// default is applied once that succeeded.
func (m *UI) deleteTheme(name string) tea.Cmd {
	if err := styles.DeleteThemeFile(name); err != nil {
		return util.ReportError(err)
	}
	deleted := func() tea.Cmd {
		m.dialog.CloseDialog(dialog.ThemeID)
		m.openThemeDialog()
		return util.ReportInfo("Deleted theme " + name)
	}
	if !strings.EqualFold(common.ThemeNameFromConfig(m.com.Config()), name) {
		return deleted()
	}
	return m.writeConfigField("options.tui.active_theme", defaultThemeName, func(err error) tea.Cmd {
		if err != nil {
			return util.ReportError(err)
		}
		newStyles, err := styles.LoadTheme(defaultThemeName)
		if err != nil {
			return util.ReportError(err)
		}
		m.applyTheme(newStyles)
		m.theme.endPreview()
		return deleted()
	})
}
