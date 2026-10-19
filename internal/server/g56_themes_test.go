package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/stretchr/testify/require"
)

// activeThemeSetting reads the terminal's options.tui.active_theme from the
// global data config file.
func activeThemeSetting(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(config.GlobalConfigData())
	require.NoError(t, err)
	var cfg struct {
		Options struct {
			TUI struct {
				ActiveTheme string `json:"active_theme"`
			} `json:"tui"`
		} `json:"options"`
	}
	require.NoError(t, json.Unmarshal(data, &cfg))
	return cfg.Options.TUI.ActiveTheme
}

// setActiveTheme sets the terminal's theme choice through the config route.
func (g *g56Workspace) setActiveTheme(t *testing.T, name string) {
	t.Helper()
	liveJSON[struct{}](t, g.h, http.MethodPost, g.path("/config/set"),
		proto.ConfigSetRequest{Scope: config.ScopeGlobal, Key: "options.tui.active_theme", Value: name}, http.StatusOK)
	require.Equal(t, name, activeThemeSetting(t))
}

// TestThemeRoutesWriteTerminalBytes: a theme saved or created through the
// routes is byte for byte the file the terminal's SaveThemeFile writes.
func TestThemeRoutesWriteTerminalBytes(t *testing.T) {
	g := newG56Workspace(t)

	palette, err := styles.ThemePalette("gruvbox-dark")
	require.NoError(t, err)
	palette.BgBase = "#123456"
	roles := map[string]string{}
	for _, f := range styles.PaletteFields() {
		roles[f.Name] = f.Get(palette)
	}
	view := liveJSON[proto.ThemeView](t, g.h, http.MethodPut, "/v1/themes/mine",
		proto.ThemePaletteRequest{Base: "gruvbox-dark", Palette: roles}, http.StatusOK)
	require.Equal(t, "mine", view.Name)
	require.Equal(t, "#123456", view.Resolved["bg_base"])
	require.NotEmpty(t, view.Revision)

	want := filepath.Join(t.TempDir(), "want.json")
	require.NoError(t, styles.SaveThemeFile(want, &styles.ThemeFile{Base: "gruvbox-dark", Palette: palette}))
	path, err := styles.FindThemeFile("mine")
	require.NoError(t, err)
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	wantBytes, err := os.ReadFile(want)
	require.NoError(t, err)
	require.Equal(t, string(wantBytes), string(got), "PUT writes SaveThemeFile's bytes")

	liveJSON[proto.ThemeView](t, g.h, http.MethodPost, "/v1/themes",
		proto.ThemeCreateRequest{Name: "fresh", Base: "graphite"}, http.StatusOK)
	exported, err := styles.ExportResolvedPalette("graphite")
	require.NoError(t, err)
	require.NoError(t, styles.SaveThemeFile(want, exported))
	path, err = styles.FindThemeFile("fresh")
	require.NoError(t, err)
	got, err = os.ReadFile(path)
	require.NoError(t, err)
	wantBytes, err = os.ReadFile(want)
	require.NoError(t, err)
	require.Equal(t, string(wantBytes), string(got), "POST writes the exported palette's bytes")

	liveError(t, g.h, http.MethodPost, "/v1/themes", proto.ThemeCreateRequest{Name: "fresh", Base: "graphite"},
		http.StatusBadRequest, "bad_request", `theme "fresh" already exists`)
	liveError(t, g.h, http.MethodPost, "/v1/themes", proto.ThemeCreateRequest{Name: "graphite"},
		http.StatusBadRequest, "bad_request", `"graphite" is a built-in theme; edit it instead`)

	list := liveJSON[proto.ThemeList](t, g.h, http.MethodGet, "/v1/themes", nil, http.StatusOK)
	var names []string
	for _, item := range list.Themes {
		names = append(names, item.Name)
	}
	require.Contains(t, names, "mine")
	require.Contains(t, names, "fresh")
}

// TestThemeResolveMatchesLoadTheme: a theme's view carries the colors the
// terminal's own LoadTheme and MergePalette resolve.
func TestThemeResolveMatchesLoadTheme(t *testing.T) {
	g := newG56Workspace(t)

	view := liveJSON[proto.ThemeView](t, g.h, http.MethodGet, "/v1/themes/gruvbox-dark", nil, http.StatusOK)
	s, err := styles.LoadTheme("gruvbox-dark")
	require.NoError(t, err)
	require.Equal(t, *s.Markdown.CodeBlock.Chroma.Keyword.Color, view.Syntax["keyword"])
	require.Equal(t, *s.Markdown.Code.Color, view.Markdown["code"])
	builtin, err := styles.ThemePalette("gruvbox-dark")
	require.NoError(t, err)
	require.Equal(t, builtin.BgBase, view.Palette["bg_base"])
	require.Len(t, view.Palette, len(styles.PaletteFields()))
	require.Equal(t, "gruvbox-dark", view.Root)
	require.True(t, view.Pure)
	require.True(t, view.Dark)
	require.Empty(t, view.Revision)

	preview := liveJSON[proto.ThemeView](t, g.h, http.MethodPost, "/v1/themes/resolve",
		proto.ThemePaletteRequest{Base: "gruvbox-dark", Palette: map[string]string{"bg_base": "#fafafa"}}, http.StatusOK)
	merged, err := styles.MergePalette("gruvbox-dark", styles.Palette{BgBase: "#fafafa"})
	require.NoError(t, err)
	require.Equal(t, merged.Primary, preview.Palette["primary"])
	require.Equal(t, "#fafafa", preview.Resolved["bg_base"])
	require.False(t, preview.Dark)
	require.Empty(t, preview.Name)
	previewStyles, err := styles.LoadPaletteTheme("gruvbox-dark", styles.Palette{BgBase: "#fafafa"})
	require.NoError(t, err)
	require.Equal(t, *previewStyles.Markdown.CodeBlock.Chroma.Keyword.Color, preview.Syntax["keyword"])

	liveError(t, g.h, http.MethodPost, "/v1/themes/resolve",
		proto.ThemePaletteRequest{Base: "gruvbox-dark", Palette: map[string]string{"bg_base": "nope"}},
		http.StatusUnprocessableEntity, "invalid_palette", `invalid palette colors: bg_base: invalid color "nope"`)
	liveError(t, g.h, http.MethodGet, "/v1/themes/missing", nil, http.StatusNotFound, "theme_not_found", "")
}

// TestRenameMovesTerminalActiveTheme: renaming the theme the terminal
// shows moves its active_theme to the new name, as the terminal does.
func TestRenameMovesTerminalActiveTheme(t *testing.T) {
	g := newG56Workspace(t)
	liveJSON[proto.ThemeView](t, g.h, http.MethodPost, "/v1/themes",
		proto.ThemeCreateRequest{Name: "mine", Base: "graphite"}, http.StatusOK)
	g.setActiveTheme(t, "mine")

	liveJSON[struct{}](t, g.h, http.MethodPost, "/v1/themes/mine/rename", proto.ThemeRenameRequest{NewName: "yours"}, http.StatusOK)
	require.Equal(t, "yours", activeThemeSetting(t))
	_, err := styles.FindThemeFile("mine")
	require.Error(t, err)
	_, err = styles.FindThemeFile("yours")
	require.NoError(t, err)

	liveJSON[proto.ThemeView](t, g.h, http.MethodPost, "/v1/themes",
		proto.ThemeCreateRequest{Name: "other", Base: "graphite"}, http.StatusOK)
	liveJSON[struct{}](t, g.h, http.MethodPost, "/v1/themes/other/rename", proto.ThemeRenameRequest{NewName: "another"}, http.StatusOK)
	require.Equal(t, "yours", activeThemeSetting(t), "renaming a theme the terminal does not show leaves its choice")
	liveError(t, g.h, http.MethodPost, "/v1/themes/gone/rename", proto.ThemeRenameRequest{NewName: "new"},
		http.StatusNotFound, "theme_not_found", "")
}

// TestDeleteThemeChangesNoSetting: deleting the theme the terminal shows
// removes only its file.
func TestDeleteThemeChangesNoSetting(t *testing.T) {
	g := newG56Workspace(t)
	liveJSON[proto.ThemeView](t, g.h, http.MethodPost, "/v1/themes",
		proto.ThemeCreateRequest{Name: "mine", Base: "graphite"}, http.StatusOK)
	g.setActiveTheme(t, "mine")

	liveJSON[struct{}](t, g.h, http.MethodDelete, "/v1/themes/mine", nil, http.StatusOK)
	_, err := styles.FindThemeFile("mine")
	require.Error(t, err)
	require.Equal(t, "mine", activeThemeSetting(t))
	liveError(t, g.h, http.MethodDelete, "/v1/themes/graphite", nil, http.StatusBadRequest, "bad_request", `"graphite" is a built-in theme`)
}
