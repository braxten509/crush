package styles

import (
	"fmt"
	"image/color"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/require"
)

// badge is a style whose text [badgeInk] picks: the text sits on fill and
// is the background role ink unless that is hard to read.
type badge struct {
	name  string
	style lipgloss.Style
	fill  color.Color
	ink   color.Color
}

func rgbHex(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

// badgesOf lists the styles filled with a status color whose text
// [badgeInk] picks. In this fork the status indicators and messages, the
// yolo mode badge and the yolo prompt mark are colored text on a subtle
// surface or the terminal background, not filled chips, so they are not
// here.
func badgesOf(s Styles, o quickStyleOpts) []badge {
	return []badge{
		{"Status.ModeBannerYolo", s.Status.ModeBannerYolo, o.yolo, o.bgBase},
		{"Button.Hovered", s.Button.Hovered, o.buttonHovered, o.onPrimary},
		{"Attachments.Image", s.Attachments.Image, o.success, o.bgLessVisible},
		{"Attachments.Text", s.Attachments.Text, o.success, o.bgLessVisible},
		{"Attachments.Skill", s.Attachments.Skill, o.success, o.bgLessVisible},
		{"Tool.AgenticFetchPromptTag", s.Tool.AgenticFetchPromptTag, o.success, o.separator},
	}
}

// TestBadgeInk_BuiltinsKeepTheirInk pins the built-in themes' look: each
// keeps the background role it always wrote on its badges where that role
// reads on the fill. Where it does not (scholars-edition's hovered button,
// cream on cream), the text must read instead.
func TestBadgeInk_BuiltinsKeepTheirInk(t *testing.T) {
	t.Parallel()
	for name, opts := range builtinThemes {
		o := opts()
		s := quickStyle(o)
		for _, b := range badgesOf(s, o) {
			if wcagContrast(b.ink, b.fill) >= minBadgeInkContrast {
				require.Equal(t, rgbHex(b.ink), rgbHex(b.style.GetForeground()), "%s: %s", name, b.name)
				continue
			}
			got := wcagContrast(b.style.GetForeground(), b.fill)
			require.GreaterOrEqual(t, got, minBadgeInkContrast, "%s: %s: %s on %s", name, b.name,
				rgbHex(b.style.GetForeground()), rgbHex(b.fill))
		}
	}
}

// TestBadgeInk_LightTheme checks the badges stay legible on a light theme,
// where the background roles they write by default are light too.
func TestBadgeInk_LightTheme(t *testing.T) {
	t.Parallel()
	light := map[string]string{
		"fg_base":             "#3A3943",
		"fg_subtle":           "#4D4C57",
		"bg_base":             "#F7F6FB",
		"bg_least_visible":    "#ECEBF0",
		"bg_less_visible":     "#D6D3DC",
		"bg_most_visible":     "#BFBCC8",
		"separator":           "#D6D3DC",
		"destructive":         "#AB2454",
		"warning":             "#BF976F",
		"warning_subtle":      "#4D4C57",
		"busy":                "#BF976F",
		"yolo":                "#4D4C57",
		"success":             "#00A475",
		"success_most_subtle": "#00A475",
		"on_primary":          "#FFFAF1",
		"button_hovered":      "#A2A0AD",
	}
	s, err := HostThemeStyles("", light, nil)
	require.NoError(t, err)

	p, _, _, err := parseHostTheme(light, nil)
	require.NoError(t, err)
	base, err := builtinThemeOpts("charmtone-panther")
	require.NoError(t, err)
	o := p.ToQuickStyleOpts(base)
	for _, b := range badgesOf(s, o) {
		got := wcagContrast(b.style.GetForeground(), b.fill)
		require.GreaterOrEqual(t, got, minBadgeInkContrast, "%s: %s on %s", b.name,
			rgbHex(b.style.GetForeground()), rgbHex(b.fill))
	}
	// The light background role that reads well stays: bgBase on the dark
	// yolo fill.
	require.Equal(t, "#f7f6fb", rgbHex(s.Status.ModeBannerYolo.GetForeground()))
}

// primaryBadgesOf lists the styles [legibleBadges] picks the text of, as
// text and fill colors.
//
// The plan mode badge and the markdown H1 are not here: in this fork they
// are colored text on the terminal background with no fill of their own
// (no chip on headings), so there is no badge text for [legibleBadges] to
// pick.
func primaryBadgesOf(s Styles) map[string][2]color.Color {
	pair := func(st lipgloss.Style) [2]color.Color {
		return [2]color.Color{st.GetForeground(), st.GetBackground()}
	}
	return map[string][2]color.Color{
		"Status.ModeBannerPlan":            pair(s.Status.ModeBannerPlan),
		"Editor.PromptQuestionIconFocused": pair(s.Editor.PromptQuestionIconFocused),
		"FilePicker.Selected":              pair(s.FilePicker.Selected),
	}
}

// TestLegibleBadges_LightTheme checks the primary-filled badges, the plan
// banner and the H1 heading stay legible on a light host or user theme.
func TestLegibleBadges_LightTheme(t *testing.T) {
	light := map[string]string{
		"primary":          "#6B50FF",
		"on_primary":       "#FFFAF1",
		"fg_base":          "#3A3943",
		"bg_base":          "#F7F6FB",
		"warning_subtle":   "#4D4C57",
		"plan_more_subtle": "#6B50FF",
	}
	setTestThemeDirs(t, []string{t.TempDir()})
	host, err := HostThemeStyles("", light, nil)
	require.NoError(t, err)

	var p Palette
	for _, f := range PaletteFields() {
		if v, ok := light[f.Name]; ok {
			f.Set(&p, v)
		}
	}
	user, err := LoadPaletteTheme("", p)
	require.NoError(t, err)

	for theme, s := range map[string]Styles{"host": host, "user": user} {
		for name, c := range primaryBadgesOf(s) {
			got := wcagContrast(c[0], c[1])
			require.GreaterOrEqual(t, got, minBadgeInkContrast, "%s theme: %s: %s on %s", theme, name,
				rgbHex(c[0]), rgbHex(c[1]))
		}
	}
}

// TestLegibleBadges_BuiltinsKeepTheirLook checks a built-in, loaded as a
// theme or mirrored by a host, keeps the ink it always wrote on these
// styles, even where it reads poorly (gruvbox-dark's H1).
func TestLegibleBadges_BuiltinsKeepTheirLook(t *testing.T) {
	setTestThemeDirs(t, []string{t.TempDir()})
	for name := range builtinThemes {
		want, err := LoadTheme(name)
		require.NoError(t, err)
		host, err := HostThemeStyles(name, nil, nil)
		require.NoError(t, err)
		got := primaryBadgesOf(host)
		for style, c := range primaryBadgesOf(want) {
			require.Equal(t, rgbHex(c[0]), rgbHex(got[style][0]), "%s: %s", name, style)
		}
	}
	// The default theme's plan banner keeps fgBase although it is below
	// 3:1 there.
	require.Equal(t, rgbHex(charmtoneOpts().fgBase), rgbHex(CharmtonePantera().Status.ModeBannerPlan.GetForeground()))
}

func TestBadgeInk(t *testing.T) {
	t.Parallel()
	dark, light := lipgloss.Color("#201F26"), lipgloss.Color("#F7F6FB")
	bright, deep := lipgloss.Color("#00FFB2"), lipgloss.Color("#AB2454")

	require.Equal(t, dark, badgeInk(bright, dark, light), "legible ink stays")
	require.Equal(t, dark, badgeInk(bright, light, dark), "illegible ink is swapped")
	require.Equal(t, light, badgeInk(deep, light, dark))
	// An alternative that reads no better is not taken.
	fill, ink := lipgloss.Color("#777777"), lipgloss.Color("#808080")
	require.Equal(t, ink, badgeInk(fill, ink, fill))
	require.Nil(t, badgeInk(bright, nil, dark))
	require.Equal(t, dark, badgeInk(nil, dark, light))
}
