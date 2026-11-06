package kittest

import (
	"image"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/stretchr/testify/require"
)

// Restylable is a dialog that can pick up the current styles after the
// theme was swapped in place.
type Restylable interface {
	Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor
	RefreshStyles()
}

// RestyleTheme returns a theme whose colors differ from the default
// throughout, built without reading theme files.
func RestyleTheme(t *testing.T) styles.Styles {
	t.Helper()
	s, err := styles.HostThemeStyles("", map[string]string{
		"primary":          "#fabd2f",
		"secondary":        "#b8bb26",
		"accent":           "#83a598",
		"fg_base":          "#ebdbb2",
		"fg_subtle":        "#d5c4a1",
		"fg_more_subtle":   "#bdae93",
		"fg_most_subtle":   "#a89984",
		"bg_base":          "#282828",
		"bg_least_visible": "#32302f",
		"bg_less_visible":  "#3c3836",
		"bg_most_visible":  "#504945",
		"separator":        "#665c54",
		"success":          "#98971a",
		"destructive":      "#fb4934",
	}, map[string]string{"keyword": "#fe8019", "text": "#ebdbb2"})
	require.NoError(t, err)
	return s
}

// Drawn renders d into a screen of the given size.
func Drawn(d Restylable, w, h int) string {
	scr := uv.NewScreenBuffer(w, h)
	d.Draw(scr, image.Rect(0, 0, w, h))
	return scr.Render()
}

// RequireRestyled checks that a dialog built under the default theme,
// drawn, then restyled after the theme was swapped in place, draws exactly
// like one built under the new theme.
func RequireRestyled[D Restylable](t *testing.T, build func(*common.Common) D) {
	t.Helper()
	const w, h = 100, 40

	old := styles.CharmtonePantera()
	com := &common.Common{Styles: &old}
	d := build(com)
	before := Drawn(d, w, h)

	// Swap the theme the way the UI does: in place, then drop the shared
	// caches keyed by the styles pointer.
	*com.Styles = RestyleTheme(t)
	common.InvalidateStyleCaches()
	d.RefreshStyles()
	after := Drawn(d, w, h)

	fresh := RestyleTheme(t)
	want := Drawn(build(&common.Common{Styles: &fresh, Workspace: com.Workspace}), w, h)
	require.NotEqual(t, before, want, "the themes must look different")
	require.Equal(t, want, after)
}
