package model

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/dialog/theme"
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

	// previewSeq numbers preview loads, which run off the Update
	// goroutine. Only the latest one is shown, and ending a preview drops
	// any still in flight.
	previewSeq int
}

// nextPreview starts a preview load and returns its number.
func (t *themeController) nextPreview() int {
	t.previewSeq++
	return t.previewSeq
}

// isLatestPreview reports whether seq is the latest preview load and no
// preview ended since it started.
func (t *themeController) isLatestPreview(seq int) bool {
	return seq == t.previewSeq
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
	t.previewSeq++
}

// takePreview returns the saved styles, if a preview is showing, and ends
// the preview.
func (t *themeController) takePreview() (styles.Styles, bool) {
	t.previewSeq++
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
		name := msg.Theme
		return m.loadThemePreview(func() (styles.Styles, error) {
			return styles.LoadTheme(name)
		}), true
	case dialog.ActionRevertThemePreview:
		m.revertThemePreview()
		m.dialog.CloseDialog(dialog.ThemeID)
	case dialog.ActionPreviewThemePalette:
		base, palette := msg.Base, msg.Palette
		return m.loadThemePreview(func() (styles.Styles, error) {
			return styles.LoadPaletteTheme(base, palette)
		}), true
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

// loadThemePreview loads a theme for preview off the Update goroutine (it
// reads the user's theme files) and shows it once loaded, unless a newer
// preview started or the preview ended meanwhile. A theme that fails to
// load is not previewed.
func (m *UI) loadThemePreview(load func() (styles.Styles, error)) tea.Cmd {
	seq := m.theme.nextPreview()
	var loaded styles.Styles
	return doThen(func() error {
		var err error
		loaded, err = load()
		return err
	}, func(err error) tea.Cmd {
		if err == nil && m.theme.isLatestPreview(seq) {
			m.showThemePreview(loaded)
		}
		return nil
	})
}

// switchTheme makes themeName the active theme. Loading the theme and the
// config write run off the Update goroutine; the theme is applied (and the
// picker closed) once both succeeded, and a failed write drops any preview.
func (m *UI) switchTheme(themeName string) tea.Cmd {
	ws := m.com.Workspace
	var (
		newStyles styles.Styles
		loadErr   error
	)
	return doThen(func() error {
		if newStyles, loadErr = styles.LoadTheme(themeName); loadErr != nil {
			return loadErr
		}
		return ws.SetConfigField(config.ScopeGlobal, "options.tui.active_theme", themeName)
	}, func(err error) tea.Cmd {
		if loadErr != nil {
			return util.ReportError(loadErr)
		}
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
// the active one, applies it. The file is written (and the theme reloaded)
// off the Update goroutine.
func (m *UI) saveThemePalette(msg dialog.ActionSaveThemePalette) tea.Cmd {
	// The theme is stored under its own name; Base only identifies the
	// built-in palette its colors are derived from.
	themeName := msg.Name
	if themeName == "" {
		themeName = msg.Base
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

	tf := &styles.ThemeFile{Base: msg.Base, Palette: msg.Palette}
	var (
		newStyles styles.Styles
		loadErr   error
	)
	return doThen(func() error {
		// Write the file before touching the UI so a failed save never
		// leaves colors applied that did not reach disk.
		savePath, err := styles.ThemePath(themeName)
		if err != nil {
			return err
		}
		if err := styles.SaveThemeFile(savePath, tf); err != nil {
			return err
		}
		if isActive {
			newStyles, loadErr = styles.LoadTheme(themeName)
		}
		return nil
	}, func(err error) tea.Cmd {
		if err != nil {
			return util.ReportError(err)
		}
		if isActive {
			if loadErr != nil {
				return util.ReportError(loadErr)
			}
			m.applyTheme(newStyles)
			m.theme.endPreview()
		}
		m.dialog.CloseDialog(dialog.ThemeEditorID)
		if td, ok := m.dialog.Dialog(dialog.ThemeID).(*theme.Theme); ok {
			td.RefreshThemes(themeName)
		}
		return util.ReportInfo("Theme saved")
	})
}

// revertOverriddenTheme drops any user override layered on top of the
// built-in theme name: the shadowing theme file and the config palette
// entry. The file is removed (and the built-in reloaded) off the Update
// goroutine.
func (m *UI) revertOverriddenTheme(name string) tea.Cmd {
	// If the reverted theme is the active one, re-apply the pristine
	// built-in so the change is visible immediately.
	isActive := strings.EqualFold(common.ThemeNameFromConfig(m.com.Config()), name)
	var (
		newStyles styles.Styles
		reloaded  bool
	)
	return doThen(func() error {
		if path, err := styles.FindThemeFile(name); err == nil {
			if err := os.Remove(path); err != nil {
				return fmt.Errorf("revert theme: %w", err)
			}
		}
		if isActive {
			var err error
			newStyles, err = styles.LoadTheme(name)
			reloaded = err == nil
		}
		return nil
	}, func(err error) tea.Cmd {
		if err != nil {
			return util.ReportError(err)
		}
		if reloaded {
			m.applyTheme(newStyles)
		}
		m.dialog.CloseDialog(dialog.ThemeID)
		m.openThemeDialog()
		return util.ReportInfo("Reverted " + name + " to its built-in colors")
	})
}

// createTheme saves a new theme named name with the palette of base and
// makes it the active theme. The theme file and config writes run off the
// Update goroutine; the editor opens once they succeeded.
func (m *UI) createTheme(base, name string) tea.Cmd {
	if base == "" {
		base = defaultThemeName
	}
	ws := m.com.Workspace
	return doThen(func() error {
		exported, err := styles.ExportResolvedPalette(base)
		if err != nil {
			// Fall back to the default theme when the base theme is no
			// longer resolvable (e.g. a user theme that was since
			// deleted).
			exported, err = styles.ExportResolvedPalette(defaultThemeName)
			if err != nil {
				return err
			}
		}
		savePath, err := styles.ThemePath(name)
		if err != nil {
			return err
		}
		// ExportResolvedPalette already pins Base to the built-in root
		// the palette was resolved from. Keep it so the new theme stays
		// loadable even if a user theme used as the source is later
		// deleted or renamed.
		if err := styles.SaveThemeFile(savePath, exported); err != nil {
			return err
		}
		return ws.SetConfigField(config.ScopeGlobal, "options.tui.active_theme", name)
	}, func(err error) tea.Cmd {
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
// follows, and a failed config write undoes the rename. Both run off the
// Update goroutine.
func (m *UI) renameTheme(oldName, newName string) tea.Cmd {
	cfg := m.com.Config()
	isActive := cfg != nil && cfg.Options != nil && cfg.Options.TUI != nil && strings.EqualFold(cfg.Options.TUI.ActiveTheme, oldName)
	ws := m.com.Workspace
	return doThen(func() error {
		oldPath, newPath, err := styles.RenameThemeFile(oldName, newName)
		if err != nil || !isActive {
			return err
		}
		if err := ws.SetConfigField(config.ScopeGlobal, "options.tui.active_theme", newName); err != nil {
			if rollbackErr := os.Rename(newPath, oldPath); rollbackErr != nil {
				slog.Error("Failed to roll back theme rename", "error", rollbackErr)
			}
			return err
		}
		return nil
	}, func(err error) tea.Cmd {
		if err != nil {
			return util.ReportError(err)
		}
		m.dialog.CloseDialog(dialog.ThemeID)
		m.openThemeDialog()
		return util.ReportInfo("Renamed theme " + oldName + " to " + newName)
	})
}

// deleteTheme deletes a user theme. When it was the active theme, the
// config falls back to the default theme, which is applied once that
// succeeded. The file and config writes run off the Update goroutine.
func (m *UI) deleteTheme(name string) tea.Cmd {
	isActive := strings.EqualFold(common.ThemeNameFromConfig(m.com.Config()), name)
	ws := m.com.Workspace
	var newStyles styles.Styles
	return doThen(func() error {
		if err := styles.DeleteThemeFile(name); err != nil || !isActive {
			return err
		}
		if err := ws.SetConfigField(config.ScopeGlobal, "options.tui.active_theme", defaultThemeName); err != nil {
			return err
		}
		var err error
		newStyles, err = styles.LoadTheme(defaultThemeName)
		return err
	}, func(err error) tea.Cmd {
		if err != nil {
			return util.ReportError(err)
		}
		if isActive {
			m.applyTheme(newStyles)
			m.theme.endPreview()
		}
		m.dialog.CloseDialog(dialog.ThemeID)
		m.openThemeDialog()
		return util.ReportInfo("Deleted theme " + name)
	})
}

// applyThemeForProvider swaps the active theme to the one associated with
// the given provider, but only when that theme differs from the one
// already applied. Most providers share a single theme, so re-selecting a
// model from the same theme family would otherwise pay the full cost of
// invalidating the markdown renderer cache and re-rendering the entire
// transcript for no visible change.
// A theme explicitly selected in the config always wins, so provider
// changes never discard the user's choice.
func (m *UI) applyThemeForProvider(providerID string) {
	// A theme the user explicitly selected always wins over the
	// per-provider default, so provider or session changes never discard
	// their choice. The in-memory flag covers client/server mode, where
	// the config round-trip may not surface the selection right away.
	if !m.theme.swapForProvider(providerID, common.ThemeNameFromConfig(m.com.Config()) != "") {
		return
	}
	m.applyTheme(styles.ThemeForProvider(providerID))
}

// applyTheme replaces the active styles with the given theme, drops the
// shared style caches, and refreshes every component that caches style
// data.
func (m *UI) applyTheme(s styles.Styles) {
	*m.com.Styles = s
	common.InvalidateStyleCaches()
	m.refreshStyles()
	m.chat.InvalidateRenderCaches()
}

// previewTheme applies the given styles for live preview inside an open
// theme dialog. The whole interface updates, but only the chat messages
// currently on screen re-render; the rest of the transcript keeps its
// cached output so browsing themes stays fast in large sessions. Off-screen
// messages follow along when the theme is actually applied.
func (m *UI) previewTheme(s styles.Styles) {
	*m.com.Styles = s
	common.InvalidateStyleCaches()
	m.refreshStyles()
	m.chat.InvalidateVisibleRenderCaches()
}

// refreshStyles pushes the current *m.com.Styles into every subcomponent
// that copies or pre-renders style-dependent values at construction time.
// Callers are responsible for invalidating chat render caches.
func (m *UI) refreshStyles() {
	t := m.com.Styles
	m.header.refresh()
	if m.layout.sidebar.Dx() > 0 {
		m.cacheSidebarLogo(m.layout.sidebar.Dx())
	}
	m.textarea.SetStyles(t.Editor.Textarea)
	m.mention.popup.SetStyles(t.Completions.Normal, t.Completions.Focused, t.Completions.Match)
	m.attachments.SetStyles(t.Attachments)
	m.todoSpinner.Style = t.Pills.TodoSpinner
	m.status.help.Styles = t.Help
	if d := m.dialog.Dialog(dialog.ThemeID); d != nil {
		if td, ok := d.(*theme.Theme); ok {
			td.RefreshStyles()
		}
	}
}

// openThemeNewDialog opens the new theme naming dialog. The new theme
// inherits its palette from the currently active theme.
func (m *UI) openThemeNewDialog() {
	if m.dialog.ContainsDialog(dialog.ThemeNewID) {
		m.dialog.BringToFront(dialog.ThemeNewID)
		return
	}
	base := common.ThemeNameFromConfig(m.com.Config())
	m.dialog.OpenDialog(theme.NewThemeNew(m.com, base))
}

// openThemeDialog opens the theme picker dialog.
func (m *UI) openThemeDialog() {
	if m.dialog.ContainsDialog(dialog.ThemeID) {
		m.dialog.BringToFront(dialog.ThemeID)
		return
	}
	themeDialog := theme.NewTheme(m.com)
	m.dialog.OpenDialog(themeDialog)
}

// openThemeEditorDialog opens the theme editor dialog for the given theme.
// An empty themeName edits the currently active theme.
func (m *UI) openThemeEditorDialog(themeName string) {
	if m.dialog.ContainsDialog(dialog.ThemeEditorID) {
		m.dialog.BringToFront(dialog.ThemeEditorID)
		return
	}
	themeDialog := theme.NewThemeEditor(m.com, themeName)
	m.dialog.OpenDialog(themeDialog)
}
