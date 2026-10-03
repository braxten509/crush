// Package logo renders a Crush wordmark in a stylized way.
package logo

import (
	"fmt"
	"image/color"
	"math"
	"math/rand/v2"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
)

// letterform represents a letterform. It can be stretched horizontally by
// a given amount via the boolean argument.
type letterform func(bool) string

// Opts are the options for rendering the Crush title art.
type Opts struct {
	TitleColorA  color.Color // left gradient ramp point
	TitleColorB  color.Color // right gradient ramp point
	CharmColor   color.Color // Charm™ text color
	VersionColor color.Color // version text color
	Width        int         // width of the rendered logo, used for truncation
	Hyper        bool        // whether it is Crush or Hypercrush

	// Flow animates the letters: a gradient through TitleColorA,
	// TitleColorB and FlowColor that drifts diagonally, looping as Flow goes
	// from 0 to 1. Without a FlowColor the letters keep the plain A to B
	// gradient.
	Flow      float64
	FlowColor color.Color

	// Shine is how far a diagonal band of light has swept across the
	// letters, from 0 to 1. 0 draws no shine. ShineColor is the light.
	Shine      float64
	ShineColor color.Color

	// When true, stretch a random letterform on each render. Has no effect in
	// compact mode. Mainly for testing. In production you will want to cache
	// the stretched letterform to keep the logo from jittering on resize.
	Unstable bool
}

// Render renders the Crush logo. Set the argument to true to render the narrow
// version, intended for use in a sidebar.
//
// The compact argument determines whether it renders compact for the sidebar
// or wider for the main pane.
func Render(base lipgloss.Style, version string, compact bool, o Opts) string {
	charm := "Charm™"

	fg := func(c color.Color, s string) string {
		return lipgloss.NewStyle().Foreground(c).Render(s)
	}

	// Title.
	const spacing = 1
	var hyperLetterforms []letterform
	if o.Hyper {
		hyperLetterforms = []letterform{
			LetterH,
			LetterYAlt,
			LetterP,
			LetterE,
			LetterR,
		}
	}
	crushLetterforms := []letterform{
		LetterC,
		LetterR,
		LetterU,
		LetterSAlt,
		LetterH,
	}
	if o.Hyper && !compact {
		crushLetterforms = append(hyperLetterforms, crushLetterforms...)
	}

	stretchIndex := -1 // -1 means no stretching.
	if !compact && !o.Unstable {
		// Always stretch the same letterform, which is picked once at random.
		stretchIndex = cachedRandN(len(crushLetterforms))
	} else if !compact && o.Unstable {
		// Stretch a random letterform on every render.
		stretchIndex = rand.IntN(len(crushLetterforms))
	}
	crush := renderWord(spacing, stretchIndex, crushLetterforms...)
	if o.Hyper && compact {
		crush = renderWord(spacing, stretchIndex, hyperLetterforms...) + "\n" + crush
	}
	crushWidth := lipgloss.Width(crush)
	rows := strings.Split(crush, "\n")
	b := new(strings.Builder)
	for y, r := range rows {
		fmt.Fprintln(b, paintRow(base, r, y, crushWidth, len(rows), o))
	}
	crush = b.String()

	// Charm and version.
	metaRowGap := 1
	maxVersionWidth := crushWidth - lipgloss.Width(charm) - metaRowGap
	version = ansi.Truncate(version, maxVersionWidth, "…") // truncate version if too long.
	if o.Hyper && compact {
		version += " "
	}
	gap := max(0, crushWidth-lipgloss.Width(charm)-lipgloss.Width(version))
	metaRow := fg(o.CharmColor, charm) + strings.Repeat(" ", gap) + fg(o.VersionColor, version)

	// Join the meta row and big Crush title.
	crush = strings.TrimSpace(metaRow + "\n" + crush)

	// Narrow version. If this is Hypercrush, this is also a stacked version.
	if compact {
		return crush + "\n"
	}

	if o.Width > 0 {
		// Truncate the logo to the specified width.
		lines := strings.Split(crush, "\n")
		for i, line := range lines {
			lines[i] = ansi.Truncate(line, o.Width, "")
		}
		crush = strings.Join(lines, "\n")
	}
	return crush
}

// shineWidth is the width of the band of light, as a share of the
// wordmark's diagonal span.
const shineWidth = 0.12

// paintRow colors one row of the wordmark: a horizontal gradient, lit by
// the shine where its diagonal band crosses the row. The band leans like
// "/", so lower rows light up a little later than the rows above them.
func paintRow(base lipgloss.Style, row string, y, width, height int, o Opts) string {
	if row == "" {
		return ""
	}
	shining := o.Shine > 0 && o.Shine < 1 && o.ShineColor != nil
	if o.FlowColor == nil && !shining {
		return styles.ApplyForegroundGrad(base, row, o.TitleColorA, o.TitleColorB)
	}
	plain := []rune(ansi.Strip(row))
	ramp := lipgloss.Blend1D(len(plain), o.TitleColorA, o.TitleColorB)
	span := float64(width + 2*height)
	center := -shineWidth + o.Shine*(1+2*shineWidth)
	var out strings.Builder
	for x, c := range ramp {
		// Cells are about twice as tall as wide, so two columns per row
		// gives a 45° diagonal.
		pos := float64(x+2*y) / span
		if o.FlowColor != nil {
			c = cycle(pos*flowRepeat-o.Flow, o.TitleColorA, o.TitleColorB, o.FlowColor)
		}
		if shining {
			if light := 1 - math.Abs(pos-center)/shineWidth; light > 0 {
				c = mix(c, o.ShineColor, math.Sqrt(light))
			}
		}
		out.WriteString(base.Foreground(c).Render(string(plain[x])))
	}
	return out.String()
}

// flowRepeat is how many times the flowing gradient's colors repeat across
// the wordmark's diagonal.
const flowRepeat = 1.0

// cycle samples a looping gradient through the given colors at u; u wraps,
// so the last color blends back into the first.
func cycle(u float64, stops ...color.Color) color.Color {
	u -= math.Floor(u)
	at := u * float64(len(stops))
	i := int(at)
	return mix(stops[i%len(stops)], stops[(i+1)%len(stops)], at-float64(i))
}

// mix blends a toward b by t, from 0 (all a) to 1 (all b).
func mix(a, b color.Color, t float64) color.Color {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	at := func(x, y uint32) uint8 {
		return uint8((float64(x)*(1-t) + float64(y)*t) / 257)
	}
	return color.RGBA{at(ar, br), at(ag, bg), at(ab, bb), 255}
}

// SmallRender renders a smaller version of the Crush logo, suitable for
// smaller windows or sidebar usage.
func SmallRender(t *styles.Styles, width int, o Opts) string {
	name := "Crush"
	if o.Hyper {
		name = "HYPERCRUSH"
	}
	charm := "Charm™"
	title := t.Logo.SmallCharm.Render(charm)
	title = fmt.Sprintf("%s %s", title, styles.ApplyBoldForegroundGrad(t.Logo.GradCanvas, name, t.Logo.SmallGradFromColor, t.Logo.SmallGradToColor))
	return ansi.Truncate(title, width, "")
}
