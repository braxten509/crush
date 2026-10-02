package dialog

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/diffreview"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func changesFixture() []diffreview.File {
	var before, after strings.Builder
	for i := 1; i <= 80; i++ {
		fmt.Fprintf(&before, "line %d\n", i)
		if i == 5 || i == 70 {
			fmt.Fprintf(&after, "changed %d\n", i)
		} else {
			fmt.Fprintf(&after, "line %d\n", i)
		}
	}
	return diffreview.Build([]diffreview.Edit{
		{Path: "/p/a.go", Before: before.String(), After: after.String(), Full: true},
		{Path: "/p/new.txt", Before: "", After: "hello " + strings.Repeat("word ", 40) + "\n", Full: true},
	})
}

func drawChanges(c *Changes, w, h int) []string {
	scr := uv.NewScreenBuffer(w, h)
	c.Draw(scr, scr.Bounds())
	return strings.Split(ansi.Strip(scr.Render()), "\n")
}

// The sheet lists the changed files, then each file's diff with its line
// numbers; long lines wrap inside the sheet.
func TestChangesDraw(t *testing.T) {
	t.Parallel()
	sty := styles.CharmtonePantera()
	c := NewChanges(&common.Common{Styles: &sty}, "3 actions", changesFixture())
	lines := drawChanges(c, 100, 60)
	out := strings.Join(lines, "\n")
	t.Log("\n" + out)

	require.Contains(t, lines[1], "Changes")
	require.Contains(t, lines[1], "✕")
	require.Contains(t, lines[2], "3 actions · 2 files · +3 −2")
	require.Contains(t, out, "2 files changed  +3 −2")
	require.Contains(t, out, "■ /p/a.go")
	require.Contains(t, out, "▾  Changed  /p/a.go")
	require.Contains(t, out, "▾  Added  /p/new.txt")
	require.Contains(t, out, "@@ -2,7 +2,7 @@")
	require.Regexp(t, `\s5\s+−\s+line 5`, out)
	require.Regexp(t, `\s5\s+\+\s+changed 5`, out)
	for _, l := range lines {
		require.LessOrEqual(t, ansi.StringWidth(l), 100)
	}
	require.Contains(t, out, "word word", "long line wrapped, not cut")
}

// Mouse: the wheel scrolls, a file in the list jumps to its diff, a file's
// header folds it, the scrollbar drags, and a click outside or on ✕ closes.
func TestChangesMouse(t *testing.T) {
	t.Parallel()
	sty := styles.CharmtonePantera()
	c := NewChanges(&common.Common{Styles: &sty}, "3 actions", changesFixture())
	const w, h = 100, 20
	drawChanges(c, w, h)
	click := func(x, y int) Action {
		return c.HandleMsg(tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: y})
	}

	c.HandleMsg(common.CoalescedWheelMsg{DeltaY: 3})
	require.Equal(t, 3, c.offset)
	c.HandleMsg(common.CoalescedWheelMsg{DeltaY: -10})
	require.Equal(t, 0, c.offset)

	// Row 4 of the body is the second file in the list.
	require.Nil(t, click(c.body.Min.X+4, c.body.Min.Y+4))
	require.Equal(t, min(c.headRow[1]-1, c.maxOffset()), c.offset, "as far as it scrolls")
	lines := drawChanges(c, w, h)
	require.Contains(t, strings.Join(lines, "\n"), "Added  /p/new.txt")

	// Scrolled into a file's diff, its header stays at the top; clicking
	// it folds the file.
	c.offset = c.headRow[0] + 5
	lines = drawChanges(c, w, h)
	require.Contains(t, lines[c.body.Min.Y], "Changed  /p/a.go")
	rows := len(c.rows)
	require.Nil(t, click(c.body.Min.X+3, c.body.Min.Y))
	require.True(t, c.folded[0])
	require.Less(t, len(c.rows), rows)
	lines = drawChanges(c, w, h)
	require.Contains(t, strings.Join(lines, "\n"), "▸  Changed  /p/a.go")

	// Dragging the scrollbar to the bottom scrolls to the end.
	c.folded[0] = false
	c.built = 0
	drawChanges(c, w, h)
	require.Nil(t, click(c.panel.Max.X-1, c.body.Min.Y))
	c.HandleMsg(tea.MouseMotionMsg{Button: tea.MouseLeft, X: c.panel.Max.X - 1, Y: c.body.Max.Y - 1})
	require.Equal(t, c.maxOffset(), c.offset)
	c.HandleMsg(tea.MouseReleaseMsg{Button: tea.MouseLeft})
	c.HandleMsg(tea.MouseMotionMsg{X: c.panel.Max.X - 1, Y: c.body.Min.Y})
	require.Equal(t, c.maxOffset(), c.offset, "released: no drag")

	require.IsType(t, ActionClose{}, click(c.closeX, c.panel.Min.Y+1))
	require.IsType(t, ActionClose{}, click(c.panel.Min.X-1, 5))
}

// Keys page through the files.
func TestChangesKeys(t *testing.T) {
	t.Parallel()
	sty := styles.CharmtonePantera()
	c := NewChanges(&common.Common{Styles: &sty}, "1 action", changesFixture())
	drawChanges(c, 100, 20)
	c.HandleMsg(tea.KeyPressMsg{Code: 'n', Text: "n"})
	require.Equal(t, c.headRow[0]-1, c.offset)
	c.HandleMsg(tea.KeyPressMsg{Code: 'n', Text: "n"})
	require.Equal(t, min(c.headRow[1]-1, c.maxOffset()), c.offset)
	c.HandleMsg(tea.KeyPressMsg{Code: 'G', Text: "G"})
	require.Equal(t, c.maxOffset(), c.offset)
	c.HandleMsg(tea.KeyPressMsg{Code: 'g', Text: "g"})
	require.Equal(t, 0, c.offset)
	require.IsType(t, ActionClose{}, c.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEscape}))
}

func TestChangesFooterFitsCompactWindows(t *testing.T) {
	t.Parallel()
	for _, width := range []int{40, 68, 104, 140} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			sty := styles.CharmtonePantera()
			c := NewChanges(&common.Common{Styles: &sty}, "1 action", changesFixture())
			lines := drawChanges(c, width, 22)
			footer := strings.Index(strings.Join(lines, "\n"), "esc close")
			require.NotEqual(t, -1, footer, "the close instruction must stay visible")
			require.NotContains(t, lines[len(lines)-1], "close", "keep text clear of the terminal's bottom edge")
			for _, line := range lines {
				require.LessOrEqual(t, ansi.StringWidth(line), width)
			}
		})
	}
}
