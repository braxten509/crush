package dialog

import (
	"image"
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// The free-text answer is drawn like the composer: "?" and the title at
// columns 0 and 2, the description in the text column, and the answer on
// a band that spans every column of its rows, with "›" in the gutter.
func TestFreeTextLooksLikeTheComposer(t *testing.T) {
	t.Parallel()
	sty := styles.CharmtonePantera()
	d := NewFreeText(&sty, question.Question{ID: "q", Type: question.TypeFreeText,
		Text: "What would you like to learn?", Description: "Name a topic."})
	d.SetFocused(true)
	const width = 100
	h := d.Height(width)
	scr := uv.NewScreenBuffer(width, h)
	d.Draw(scr, image.Rect(0, 0, width, h))

	lines := strings.Split(ansi.Strip(scr.Render()), "\n")
	require.True(t, strings.HasPrefix(lines[0], "? What would you like to learn?"), lines[0])
	require.Nil(t, scr.CellAt(0, 0).Style.Bg, "no chip behind the mark")
	require.True(t, strings.HasPrefix(lines[2], "  Name a topic."), "description in the text column: %q", lines[2])

	band := sty.Editor.Textarea.Focused.Base.GetBackground()
	bandRows := 0
	for y := range h {
		if scr.CellAt(0, y).Style.Bg == nil {
			continue
		}
		bandRows++
		for x := range width {
			bg := scr.CellAt(x, y).Style.Bg
			require.NotNil(t, bg, "band gap at (%d,%d)", x, y)
			wr, wg, wb, _ := band.RGBA()
			r, g, b, _ := bg.RGBA()
			require.Equal(t, [3]uint32{wr, wg, wb}, [3]uint32{r, g, b}, "band color at (%d,%d)", x, y)
		}
	}
	require.Equal(t, 3, bandRows, "a 3-row band like the composer")
	require.Contains(t, ansi.Strip(scr.Render()), "› Type your answer...")
}
