package anim

import (
	"crypto/sha256"
	"fmt"
	"image/color"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"
	"github.com/stretchr/testify/require"
)

// dumpAnim prints everything buildFrames produces, then the rendered output
// of the first frames. The styled frames are long, so they are summarized
// by a hash; the plain rendered text is kept readable.
func dumpAnim(a *Anim, frames int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "width=%d cycling=%d labelWidth=%d births=%v\n", a.width, a.cyclingCharWidth, a.labelWidth, a.birthSteps)
	fmt.Fprintf(&b, "label=%x\nellipsis=%q\n", sha256.Sum256([]byte(strings.Join(a.label.Copy(), "|"))), a.ellipsisFrames.Copy())
	for i, f := range a.initialFrames {
		fmt.Fprintf(&b, "initial[%d]=%x\n", i, sha256.Sum256([]byte(strings.Join(f, "|"))))
	}
	for i, f := range a.cyclingFrames {
		fmt.Fprintf(&b, "cycling[%d]=%x %q\n", i, sha256.Sum256([]byte(strings.Join(f, "|"))), ansi.Strip(strings.Join(f, "")))
	}
	for i := range frames {
		out := a.Render()
		fmt.Fprintf(&b, "render[%d]=%x %q\n", i, sha256.Sum256([]byte(out)), ansi.Strip(out))
		a.Advance()
	}
	return b.String()
}

// TestBuildFramesOutput pins the pre-rendered frames, birth schedule and
// rendered output for a range of settings, on both a cache miss and a
// cache hit, and after a color change.
func TestBuildFramesOutput(t *testing.T) {
	t.Parallel()

	red := color.RGBA{R: 0xff, A: 0xff}
	green := color.RGBA{G: 0xff, A: 0xff}
	blue := color.RGBA{B: 0xff, A: 0xff}
	cases := []Settings{
		{ID: "a", Size: 4, Label: "frames-golden-plain"},
		{ID: "b", Size: 6, Label: "frames-golden-colors", LabelColor: red, GradColorA: green, GradColorB: blue},
		{ID: "c", Size: 3, Label: "frames-golden-cycle", CycleColors: true, GradColorA: red, GradColorB: blue},
		{ID: "d", Size: 5},
		{ID: "e", Label: "frames-golden-noscramble", NoScramble: true, LabelColor: green},
		{ID: "f", Size: 2, Label: "frames-golden-ünïcode 漢字", SuffixColor: blue},
	}

	var b strings.Builder
	for _, opts := range cases {
		fmt.Fprintf(&b, "=== %+v\n", opts)
		first := dumpAnim(New(opts), 30)
		require.Equal(t, first, dumpAnim(New(opts), 30), "a cache hit builds the same frames")
		b.WriteString(first)

		a := New(opts)
		a.SetColors(blue, red, green, nil)
		b.WriteString("--- recolored\n")
		b.WriteString(dumpAnim(a, 25))
	}
	golden.RequireEqual(t, []byte(b.String()))
}
