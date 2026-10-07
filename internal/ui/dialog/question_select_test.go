package dialog

import (
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// drawSelectForm draws a one-question form and returns its plain rows.
func drawSelectForm(t *testing.T, f *QuestionForm, widths ...int) (uv.ScreenBuffer, []string) {
	t.Helper()
	width := 80
	if len(widths) > 0 {
		width = widths[0]
	}
	h := f.Height(width)
	scr := uv.NewScreenBuffer(width, h)
	f.Draw(scr, image.Rect(0, 0, width, h))
	return scr, strings.Split(ansi.Strip(scr.Render()), "\n")
}

func newSelectForm(desc string) *QuestionForm {
	sty := styles.CharmtonePantera()
	f := NewQuestionForm(&sty, question.Request{Questions: []question.Question{{
		ID: "q", Type: question.TypeSingleChoice, Text: "How does it look?",
		Description: desc,
		Choices: []question.Choice{
			{ID: "a", Label: "Looks good, build it"},
			{ID: "b", Label: "Change some things"},
		},
	}}})
	f.SetFocused(true)
	return f
}

// rowOf returns the index of the first row containing s.
func rowOf(t *testing.T, lines []string, s string) (int, int) {
	t.Helper()
	for y, l := range lines {
		if x := strings.Index(l, s); x >= 0 {
			return ansi.StringWidth(l[:x]), y
		}
	}
	t.Fatalf("%q not drawn in %q", s, lines)
	return 0, 0
}

// The "?" tag starts in the text column, like the description and choices.
func TestQuestionTagInTextColumn(t *testing.T) {
	t.Parallel()
	f := newSelectForm("Some details.")
	scr, lines := drawSelectForm(t, f)
	require.Nil(t, scr.CellAt(1, 0).Style.Bg, "gutter before the tag")
	require.NotNil(t, scr.CellAt(2, 0).Style.Bg, "tag starts at the text column")
	descX, _ := rowOf(t, lines, "Some details.")
	choiceX, _ := rowOf(t, lines, "Looks good")
	require.Equal(t, 2, descX)
	require.Equal(t, 2, choiceX)
}

// Dragging selects and copies the text without gutter or tag, and does not
// pick the choice the drag crossed.
func TestQuestionFormDragSelectsText(t *testing.T) {
	t.Parallel()
	f := newSelectForm("Some details.")
	_, lines := drawSelectForm(t, f)
	_, titleY := rowOf(t, lines, "How does it look?")
	x, y := rowOf(t, lines, "Looks good, build it")

	require.True(t, f.HandleMouseDown(0, titleY))
	require.True(t, f.HandleMouseDrag(x+9, y))
	handled, cmd := f.HandleMouseRelease(x+9, y)
	require.True(t, handled)
	require.NotNil(t, cmd, "the selection is copied")
	require.Equal(t, "How does it look?\n\nSome details.\n\nLooks good", f.sel.text(f.Styles))
	require.False(t, f.TakeReleaseDone())
	require.Nil(t, f.answers[0], "a drag never answers")

	// The highlight stays until the next key or press.
	scr, _ := drawSelectForm(t, f)
	require.Equal(t, f.Styles.TextSelection.GetBackground(), scr.CellAt(x, y).Style.Bg)
	f.sel.clear()
}

// A press and release in place is still a normal click.
func TestQuestionFormClickStillPicks(t *testing.T) {
	t.Parallel()
	f := newSelectForm("")
	_, lines := drawSelectForm(t, f)
	x, y := rowOf(t, lines, "Change some things")
	require.True(t, f.HandleMouseDown(x, y))
	handled, cmd := f.HandleMouseRelease(x, y)
	require.True(t, handled)
	require.Nil(t, cmd)
	require.NotNil(t, f.answers[0], "the click picks the choice")
	require.Equal(t, []string{"b"}, f.answers[0].SelectedIDs)
}

// Clicking a Markdown link or a bare path that exists opens it.
func TestQuestionFormClickOpensLinks(t *testing.T) {
	t.Parallel()
	file := filepath.Join(t.TempDir(), "prototype.png")
	require.NoError(t, os.WriteFile(file, nil, 0o600))

	for _, tc := range []struct{ desc, click, want string }{
		{"See [the docs](https://example.com/docs) first.", "the docs", "https://example.com/docs"},
		{"Open https://example.com/a now.", "example.com", "https://example.com/a"},
		{"Image: " + file + " (5 screens).", "prototype.png", file},
	} {
		f := newSelectForm(tc.desc)
		// This test exercises unwrapped links; macOS temp paths are long.
		_, lines := drawSelectForm(t, f, max(80, len(tc.desc)+10))
		x, y := rowOf(t, lines, tc.click)
		require.True(t, f.HandleMouseDown(x+1, y))
		handled, cmd := f.HandleMouseRelease(x+1, y)
		require.True(t, handled)
		require.NotNil(t, cmd, tc.desc)
		require.Equal(t, OpenLinkMsg{URL: tc.want}, cmd(), tc.desc)
		require.False(t, f.TakeReleaseDone())
	}

	// A path that doesn't exist is just text: the click does nothing special.
	f := newSelectForm("Image: /no/such/file.png here.")
	_, lines := drawSelectForm(t, f)
	x, y := rowOf(t, lines, "/no/such")
	f.HandleMouseDown(x+1, y)
	_, cmd := f.HandleMouseRelease(x+1, y)
	require.Nil(t, cmd)
}

// Presses outside the drawn form are left to the chat and composer.
func TestQuestionFormIgnoresOutsidePress(t *testing.T) {
	t.Parallel()
	f := newSelectForm("")
	drawSelectForm(t, f)
	require.False(t, f.HandleMouseDown(5, 500))
	handled, _ := f.HandleMouseRelease(5, 500)
	require.False(t, handled)
}

// A click that finishes the form on release is reported to the UI once.
func TestQuestionFormReleaseDone(t *testing.T) {
	t.Parallel()
	sty := styles.CharmtonePantera()
	f := NewQuestionForm(&sty, question.Request{Questions: []question.Question{{
		ID: "q", Type: question.TypeYesNo, Text: "Ship it?",
	}}})
	f.SetFocused(true)
	_, lines := drawSelectForm(t, f)
	x, y := rowOf(t, lines, "Yes")
	require.True(t, f.HandleMouseDown(x, y))
	handled, _ := f.HandleMouseRelease(x, y)
	require.True(t, handled)
	require.True(t, f.TakeReleaseDone())
	require.False(t, f.TakeReleaseDone(), "reported once")
}
