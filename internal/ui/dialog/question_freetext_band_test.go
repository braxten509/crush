package dialog

import (
	"image"
	"image/color"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// requireAnswerBox checks the thin answer box (see answerPad) on rows
// top..top+2 of scr: half-block padding above and below one band row,
// all starting after the 2-cell gutter, with the lit bar in the gutter.
func requireAnswerBox(t *testing.T, sty styles.Styles, scr uv.ScreenBuffer, top, right int) {
	t.Helper()
	band := answerBand(&sty)
	same := func(a, b color.Color) bool {
		if a == nil || b == nil {
			return a == b
		}
		ar, ag, ab, _ := a.RGBA()
		br, bg, bb, _ := b.RGBA()
		return ar == br && ag == bg && ab == bb
	}
	for y, block := range map[int]string{top: "▄", top + 2: "▀"} {
		require.Equal(t, "┃", scr.CellAt(0, y).Content, "bar beside the padding")
		for x := 2; x < right; x++ {
			cell := scr.CellAt(x, y)
			require.Equal(t, block, cell.Content, "padding at (%d,%d)", x, y)
			require.True(t, same(band, cell.Style.Fg), "padding color at (%d,%d)", x, y)
			require.Nil(t, cell.Style.Bg, "half the padding row stays clear at (%d,%d)", x, y)
		}
	}
	require.Equal(t, "┃", scr.CellAt(0, top+1).Content, "bar beside the text")
	require.Nil(t, scr.CellAt(0, top+1).Style.Bg, "no band under the gutter")
	for x := 2; x < right; x++ {
		require.True(t, same(band, scr.CellAt(x, top+1).Style.Bg), "band gap at (%d,%d)", x, top+1)
	}
}

// The free-text answer sits in a thin box beside the bar, so it reads
// apart from the composer; the "?" is on a colored tag.
func TestFreeTextAnswerBox(t *testing.T) {
	t.Parallel()
	sty := styles.CharmtonePantera()
	d := NewFreeText(&sty, question.Question{ID: "q", Type: question.TypeFreeText,
		Text: "What would you like to learn?", Description: "Name a topic."})
	d.SetFocused(true)
	d.editor.SetValue("Go")
	const width = 100
	h := d.Height(width)
	scr := uv.NewScreenBuffer(width, h)
	cur := d.Draw(scr, image.Rect(0, 0, width, h))

	lines := strings.Split(ansi.Strip(scr.Render()), "\n")
	require.True(t, strings.HasPrefix(lines[0], "  ?  What would you like to learn?"), lines[0])
	require.Nil(t, scr.CellAt(0, 0).Style.Bg, "a space before the tag")
	require.NotNil(t, scr.CellAt(2, 0).Style.Bg, "the ? sits on a tag")
	require.True(t, strings.HasPrefix(lines[2], "  Name a topic."), "description in the text column: %q", lines[2])

	requireAnswerBox(t, sty, scr, 4, width)
	require.Equal(t, "┃  Go", strings.TrimRight(lines[5], " "))
	require.NotNil(t, cur)
	require.Equal(t, 5, cur.Y)
	require.Equal(t, 5, cur.X, "cursor right after the text")
	require.Equal(t, h, 8, "question, blank, description, blank, 3-row box, trailing blank")
}

// The "Something else?" fill-in uses the same thin box, editing or not,
// and the cursor lands right after the typed text.
func TestFillInAnswerBox(t *testing.T) {
	t.Parallel()
	const width, height = 60, 30
	sty := styles.CharmtonePantera()
	draw := func(d *SingleChoice) (uv.ScreenBuffer, []string, int, *tea.Cursor) {
		scr := uv.NewScreenBuffer(width, height)
		cur := d.Draw(scr, image.Rect(0, 0, width, height))
		lines := strings.Split(ansi.Strip(scr.Render()), "\n")
		top := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "▄") })
		require.GreaterOrEqual(t, top, 0)
		return scr, lines, top, cur
	}

	d := newTestSingleChoice(t)
	d.cursorIdx = len(d.Request.Choices)
	_, lines, top, _ := draw(d)
	require.Contains(t, lines[top+1], "Something else?")

	d.fillIn.Focus()
	d.fillIn.SetValue("abc")
	scr, lines, editingTop, cur := draw(d)
	require.Equal(t, top, editingTop, "the box keeps its place when typing starts")
	requireAnswerBox(t, sty, scr, top, min(width-4, choiceListMaxWidth))
	require.Equal(t, "┃  abc", strings.TrimRight(lines[top+1], " "))
	require.NotNil(t, cur)
	require.Equal(t, top+1, cur.Y)
	require.Equal(t, 6, cur.X, "cursor right after the text")
}
