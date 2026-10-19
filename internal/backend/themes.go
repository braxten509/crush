package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"strings"

	"charm.land/glamour/v2/ansi"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/ui/styles"
)

// Theme routes (desktop design 5-6, 3.2; G-2). Each calls the terminal's
// own functions in internal/ui/styles, so files are byte for byte what the
// terminal writes. No route reads or writes a theme choice, except that a
// rename moves the terminal's options.tui.active_theme with its file, as
// the terminal's own rename does.

// Theme error kinds. A theme error's text is the terminal's.
var (
	ErrThemeInvalid  = errors.New("invalid theme request")
	ErrThemeNotFound = errors.New("theme not found")
	ErrThemePalette  = errors.New("invalid palette")
)

// kindError carries the terminal's text and the kind it is.
type kindError struct {
	kind error
	err  error
}

func (e *kindError) Error() string { return e.err.Error() }
func (e *kindError) Unwrap() []error {
	return []error{e.kind, e.err}
}

func kindErr(kind, err error) error {
	if err == nil {
		return nil
	}
	return &kindError{kind: kind, err: err}
}

// desktopDefaultTheme is the desktop's theme when none is chosen (3.3); a
// new theme with no base, or whose base no longer resolves, starts from it.
const desktopDefaultTheme = "scholars-edition"

// themeActiveKey is the terminal's theme choice in the config.
const themeActiveKey = "options.tui.active_theme"

// ListThemes lists the built-in and user themes, as both apps show them.
func (b *Backend) ListThemes() proto.ThemeList {
	out := proto.ThemeList{Themes: []proto.ThemeListItem{}}
	for _, info := range styles.ListAllThemes() {
		item := proto.ThemeListItem{Name: info.Name, Source: info.Source.String(), Overridden: info.Overridden}
		if palette, _, err := themeEditorPalette(info.Name); err == nil {
			item.Dark = paletteIsDark(palette)
		}
		out.Themes = append(out.Themes, item)
	}
	return out
}

// Theme returns one theme's view.
func (b *Backend) Theme(name string) (proto.ThemeView, error) {
	return themeView(name)
}

// ResolveTheme previews unsaved colors over a base.
func (b *Backend) ResolveTheme(req proto.ThemePaletteRequest) (proto.ThemeView, error) {
	palette, err := paletteFromRequest(req.Palette)
	if err != nil {
		return proto.ThemeView{}, err
	}
	merged, err := mergeThemePalette(req.Base, palette)
	if err != nil {
		return proto.ThemeView{}, err
	}
	s, err := styles.LoadPaletteTheme(req.Base, palette)
	if err != nil {
		return proto.ThemeView{}, kindErr(ErrThemePalette, err)
	}
	view := proto.ThemeView{Base: req.Base, Root: themeRoot(req.Base)}
	fillThemeColors(&view, merged, s)
	return view, nil
}

// SaveTheme writes the editor's palette as the theme's file, as the
// terminal's editor does, and returns the saved theme.
func (b *Backend) SaveTheme(name string, req proto.ThemePaletteRequest) (proto.ThemeView, error) {
	palette, err := paletteFromRequest(req.Palette)
	if err != nil {
		return proto.ThemeView{}, err
	}
	if _, err := mergeThemePalette(req.Base, palette); err != nil {
		return proto.ThemeView{}, err
	}
	path, err := styles.ThemePath(name)
	if err != nil {
		return proto.ThemeView{}, kindErr(ErrThemeInvalid, err)
	}
	if err := styles.SaveThemeFile(path, &styles.ThemeFile{Base: req.Base, Palette: palette}); err != nil {
		return proto.ThemeView{}, err
	}
	b.themesChanged()
	return themeView(name)
}

// CreateTheme saves a new theme with the resolved palette of base, as the
// terminal's New Theme does, without choosing it.
func (b *Backend) CreateTheme(req proto.ThemeCreateRequest) (proto.ThemeView, error) {
	if err := styles.ValidateThemeName(req.Name); err != nil {
		return proto.ThemeView{}, kindErr(ErrThemeInvalid, err)
	}
	base := req.Base
	if base == "" {
		base = desktopDefaultTheme
	}
	exported, err := styles.ExportResolvedPalette(base)
	if err != nil {
		// Fall back to the default theme when the base no longer
		// resolves (a user theme since deleted), as the terminal does.
		if exported, err = styles.ExportResolvedPalette(desktopDefaultTheme); err != nil {
			return proto.ThemeView{}, err
		}
	}
	path, err := styles.ThemePath(req.Name)
	if err != nil {
		return proto.ThemeView{}, kindErr(ErrThemeInvalid, err)
	}
	if err := styles.SaveThemeFile(path, exported); err != nil {
		return proto.ThemeView{}, err
	}
	b.themesChanged()
	return themeView(req.Name)
}

// RenameTheme renames a user theme's file. When the terminal's
// active_theme names the old theme it follows to the new name; the file
// is moved back when that write fails.
func (b *Backend) RenameTheme(oldName, newName string) error {
	store := b.themeConfigStore()
	active := ""
	if store != nil {
		if cfg := store.Config(); cfg != nil && cfg.Options != nil && cfg.Options.TUI != nil {
			active = cfg.Options.TUI.ActiveTheme
		}
	}
	oldPath, newPath, err := styles.RenameThemeFile(oldName, newName)
	if err != nil {
		if styles.ValidateThemeRename(oldName, newName) != nil {
			return kindErr(ErrThemeInvalid, err)
		}
		if _, ferr := styles.FindThemeFile(oldName); ferr != nil {
			return kindErr(ErrThemeNotFound, err)
		}
		return err
	}
	if store != nil && strings.EqualFold(active, oldName) {
		if err := store.SetConfigField(config.ScopeGlobal, themeActiveKey, strings.ToLower(newName)); err != nil {
			if rollbackErr := os.Rename(newPath, oldPath); rollbackErr != nil {
				slog.Error("Failed to roll back theme rename", "error", rollbackErr)
			}
			return err
		}
	}
	b.themesChanged()
	return nil
}

// DeleteTheme deletes a user theme's file. No setting changes.
func (b *Backend) DeleteTheme(name string) error {
	if styles.IsBuiltinTheme(name) {
		return kindErr(ErrThemeInvalid, styles.DeleteThemeFile(name))
	}
	if _, err := styles.FindThemeFile(name); err != nil {
		return kindErr(ErrThemeNotFound, styles.DeleteThemeFile(name))
	}
	if err := styles.DeleteThemeFile(name); err != nil {
		return err
	}
	b.themesChanged()
	return nil
}

// RevertTheme removes the user file that shadows a built-in, as the
// terminal's revert does.
func (b *Backend) RevertTheme(name string) error {
	if !styles.IsBuiltinTheme(name) {
		return kindErr(ErrThemeInvalid, fmt.Errorf("%q is not a built-in theme", strings.ToLower(name)))
	}
	path, err := styles.FindThemeFile(name)
	if err != nil {
		return nil
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("revert theme: %w", err)
	}
	b.themesChanged()
	return nil
}

// themesChanged tells every app of the engine that a theme file changed.
func (b *Backend) themesChanged() {
	for _, ws := range b.workspaces.Seq2() {
		sendConfigChanged(ws)
	}
}

// themeConfigStore is a config store of this engine for the global
// config: a workspace's, else the server's.
func (b *Backend) themeConfigStore() *config.ConfigStore {
	for _, ws := range b.workspaces.Seq2() {
		if ws.Cfg != nil {
			return ws.Cfg
		}
	}
	return b.cfg
}

// themeView builds the view of the named theme.
func themeView(name string) (proto.ThemeView, error) {
	palette, base, err := themeEditorPalette(name)
	if err != nil {
		return proto.ThemeView{}, err
	}
	s, err := styles.LoadTheme(name)
	if err != nil {
		return proto.ThemeView{}, kindErr(ErrThemeNotFound, err)
	}
	builtin := styles.IsBuiltinTheme(name)
	view := proto.ThemeView{
		Name:   name,
		Root:   themeRoot(name),
		Base:   base,
		Source: styles.ThemeSourceUser.String(),
	}
	if builtin {
		view.Source = styles.ThemeSourceBuiltin.String()
	}
	if path, err := styles.FindThemeFile(name); err == nil {
		if info, err := os.Stat(path); err == nil {
			view.Revision = fmt.Sprintf("%d-%d", info.Size(), info.ModTime().UnixNano())
		}
		view.Overridden = builtin
	} else {
		view.Pure = builtin
	}
	fillThemeColors(&view, palette, s)
	return view, nil
}

// themeEditorPalette returns the theme's palette as the terminal's theme
// editor starts it, and its base: the user file merged with its base, else
// the built-in.
func themeEditorPalette(name string) (styles.Palette, string, error) {
	if path, err := styles.FindThemeFile(name); err == nil {
		tf, err := styles.LoadThemeFile(path)
		if err != nil {
			return styles.Palette{}, "", kindErr(ErrThemePalette, err)
		}
		base := tf.Base
		if base == "" {
			base = "charmtone-panther"
		}
		merged, err := styles.MergePalette(base, tf.Palette)
		if err != nil {
			return styles.Palette{}, "", kindErr(ErrThemePalette, err)
		}
		return merged, base, nil
	}
	p, err := styles.ThemePalette(name)
	if err != nil {
		return styles.Palette{}, "", kindErr(ErrThemeNotFound, err)
	}
	return p, strings.ToLower(name), nil
}

// mergeThemePalette is styles.MergePalette with its errors sorted: a bad
// color is ErrThemePalette, an unknown base ErrThemeNotFound.
func mergeThemePalette(base string, palette styles.Palette) (styles.Palette, error) {
	if err := palette.Validate(); err != nil {
		return styles.Palette{}, kindErr(ErrThemePalette, err)
	}
	merged, err := styles.MergePalette(base, palette)
	if err != nil {
		return styles.Palette{}, kindErr(ErrThemeNotFound, err)
	}
	return merged, nil
}

// themeRoot returns the built-in a theme's chain ends at, or "" when it
// does not resolve.
func themeRoot(name string) string {
	exported, err := styles.ExportResolvedPalette(name)
	if err != nil {
		return ""
	}
	return exported.Base
}

// paletteFromRequest builds a palette from role names; an unknown role is
// refused.
func paletteFromRequest(roles map[string]string) (styles.Palette, error) {
	var p styles.Palette
	fields := make(map[string]styles.PaletteField, len(roles))
	for _, f := range styles.PaletteFields() {
		fields[f.Name] = f
	}
	for role, value := range roles {
		f, ok := fields[role]
		if !ok {
			return styles.Palette{}, kindErr(ErrThemePalette, fmt.Errorf("invalid palette colors: %s: unknown role", role))
		}
		f.Set(&p, value)
	}
	return p, nil
}

// fillThemeColors fills the view's palette, resolved, syntax and markdown
// colors and its dark flag.
func fillThemeColors(view *proto.ThemeView, palette styles.Palette, s styles.Styles) {
	view.Palette = make(map[string]string)
	view.Resolved = make(map[string]string)
	for _, f := range styles.PaletteFields() {
		v := f.Get(palette)
		view.Palette[f.Name] = v
		view.Resolved[f.Name] = colorHex(v)
	}
	view.Dark = paletteIsDark(palette)
	view.Syntax = map[string]string{}
	if s.Markdown.CodeBlock.Chroma != nil {
		view.Syntax = styleColors(s.Markdown.CodeBlock.Chroma)
	}
	view.Markdown = styleColors(s.Markdown)
}

// colorHex resolves a palette value (hex, ANSI index or Charmtone name) to
// #rrggbb, or "" when it is empty or unknown.
func colorHex(v string) string {
	cs := styles.ColorString(v)
	if cs == "" {
		return ""
	}
	r, g, b, _ := lipgloss.Color(cs).RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

// paletteIsDark reports whether the theme's background is dark: a WCAG
// relative luminance of 0.18 or less, the line section 3.6 adjusts by.
func paletteIsDark(p styles.Palette) bool {
	cs := styles.ColorString(p.BgBase)
	if cs == "" {
		return true
	}
	r, g, b, _ := lipgloss.Color(cs).RGBA()
	lin := func(c uint32) float64 {
		v := float64(c>>8) / 255
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r)+0.7152*lin(g)+0.0722*lin(b) <= 0.18
}

// styleColors lists the foreground color of each top-level entry of a
// glamour style value, by its JSON name.
func styleColors(v any) map[string]string {
	out := map[string]string{}
	data, err := json.Marshal(v)
	if err != nil {
		return out
	}
	var entries map[string]json.RawMessage
	if json.Unmarshal(data, &entries) != nil {
		return out
	}
	for key, raw := range entries {
		var prim ansi.StylePrimitive
		if json.Unmarshal(raw, &prim) == nil && prim.Color != nil && *prim.Color != "" {
			out[key] = *prim.Color
		}
	}
	return out
}
