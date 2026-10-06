package dialog

import (
	"image"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// The y and n keys answer with the key pressed, whatever is highlighted.
func TestYesNoFormKeysAnswerDirectly(t *testing.T) {
	for _, tc := range []struct {
		name   string
		keys   []tea.KeyPressMsg
		wantOK bool
	}{
		{"y with No highlighted", []tea.KeyPressMsg{{Code: 'y', Text: "y"}}, true},
		{"n with Yes highlighted", []tea.KeyPressMsg{{Code: tea.KeyRight}, {Code: 'n', Text: "n"}}, false},
		{"enter answers the highlight", []tea.KeyPressMsg{{Code: tea.KeyRight}, {Code: tea.KeyEnter}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := styles.CharmtonePantera()
			f := NewQuestionForm(&s, question.Request{Questions: []question.Question{{
				ID: "q", Type: question.TypeYesNo, Text: "Install?", Description: "d",
			}}})
			var got *bool
			ran := false
			f.OnAnswerCmd = func(r []question.Answer) tea.Cmd {
				got = r[0].Yes
				return func() tea.Msg { ran = true; return nil }
			}
			var done bool
			var cmd tea.Cmd
			for _, k := range tc.keys {
				done, cmd = f.HandleKey(k)
			}
			require.True(t, done)
			require.NotNil(t, got)
			require.Equal(t, tc.wantOK, *got)
			require.NotNil(t, cmd, "OnAnswerCmd's command is returned")
			if msg := cmd(); msg != nil {
				for _, c := range msg.(tea.BatchMsg) {
					if c != nil {
						c()
					}
				}
			}
			require.True(t, ran)
		})
	}
}

// The "?" tag, the description and the buttons share the text column,
// and a wrapped question lines up under its first word.
func TestYesNoLinesUpUnderTag(t *testing.T) {
	t.Parallel()
	sty := styles.CharmtonePantera()
	d := NewYesNo(&sty, question.Question{ID: "q", Type: question.TypeYesNo,
		Text: "May I change this one file for you now?", Description: "Only that file."})
	const width = 30
	h := d.Height(width)
	scr := uv.NewScreenBuffer(width, h)
	d.Draw(scr, image.Rect(0, 0, width, h))
	lines := strings.Split(ansi.Strip(scr.Render()), "\n")

	require.Nil(t, scr.CellAt(1, 0).Style.Bg, "the gutter before the tag")
	require.NotNil(t, scr.CellAt(2, 0).Style.Bg, "the tag starts in the text column")
	require.True(t, strings.HasPrefix(lines[0], "   ?  May I"), lines[0])
	require.True(t, strings.HasPrefix(lines[1], "      "), "wrapped question under its first word: %q", lines[1])
	require.NotEqual(t, ' ', rune(lines[1][6]), "wrapped question under its first word: %q", lines[1])

	descRow := 3
	require.True(t, strings.HasPrefix(lines[descRow], "  Only that file."), "description in the text column: %q", lines[descRow])

	buttonRow := descRow + 2
	require.Contains(t, lines[buttonRow], "Yes")
	require.Nil(t, scr.CellAt(1, buttonRow).Style.Bg, "the gutter before the buttons")
	require.NotNil(t, scr.CellAt(2, buttonRow).Style.Bg, "the Yes button starts in the text column")
	answered, _ := d.HandleMouseClick(2, buttonRow)
	require.True(t, answered, "a click on the moved Yes button still answers")
}
