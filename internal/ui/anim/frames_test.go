package anim

import (
	"image/color"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// renderSequence returns the plain text of the first frames an Anim draws.
func renderSequence(a *Anim, frames int) []string {
	out := make([]string, 0, frames)
	for range frames {
		out = append(out, ansi.Strip(a.Render()))
		a.Advance()
	}
	return out
}

// TestBuildFramesOutput checks that the frame cache hands back the frames
// built for the same settings, and never frames built for other settings.
func TestBuildFramesOutput(t *testing.T) {
	t.Parallel()

	red := color.RGBA{R: 0xff, A: 0xff}
	green := color.RGBA{G: 0xff, A: 0xff}
	blue := color.RGBA{B: 0xff, A: 0xff}
	cases := []Settings{
		{ID: "a", Size: 4, Label: "frames-plain"},
		{ID: "b", Size: 6, Label: "frames-colors", LabelColor: red, GradColorA: green, GradColorB: blue},
		{ID: "c", Size: 3, Label: "frames-cycle", CycleColors: true, GradColorA: red, GradColorB: blue},
		{ID: "d", Size: 5},
		{ID: "e", Label: "frames-noscramble", NoScramble: true, LabelColor: green},
		{ID: "f", Size: 2, Label: "frames-ünïcode 漢字", SuffixColor: blue},
	}

	const frames = 30
	seen := map[string]string{}
	for _, opts := range cases {
		first := renderSequence(New(opts), frames)
		require.Equal(t, first, renderSequence(New(opts), frames), "a cache hit draws the same frames for %+v", opts)

		joined := strings.Join(first, "\n")
		if opts.Label != "" {
			require.Contains(t, joined, opts.Label, "the frames show their own label")
		}
		for label, other := range seen {
			if label != opts.Label {
				require.NotEqual(t, other, joined, "labels %q and %q draw differently", label, opts.Label)
			}
		}
		seen[opts.Label] = joined
	}
}
