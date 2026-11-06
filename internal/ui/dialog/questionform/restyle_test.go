package questionform

import (
	"testing"

	"charm.land/bubbles/v2/textarea"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/dialog/internal/kittest"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// Every inline editor that copies styles must be restyled when the theme
// changes in place.
var (
	_ dialog.StyleRefresher = (*QuestionForm)(nil)
	_ dialog.StyleRefresher = (*SingleChoice)(nil)
	_ dialog.StyleRefresher = (*MultiChoice)(nil)
	_ dialog.StyleRefresher = (*YesNo)(nil)
	_ dialog.StyleRefresher = (*FreeText)(nil)
	_ dialog.StyleRefresher = (*PlanHandoffInline)(nil)
)

// TestRefreshStyles_QuestionForm checks an inline question form restyles
// its text areas.
func TestRefreshStyles_QuestionForm(t *testing.T) {
	t.Parallel()
	old := styles.CharmtonePantera()
	form := NewQuestionForm(&old, question.Request{})
	single := NewSingleChoice(&old, question.Question{})
	form.questions = append(form.questions, single, NewFreeText(&old, question.Question{}))

	fresh := kittest.RestyleTheme(t)
	old = fresh
	form.RefreshStyles()

	want := questionTextareaStyles(&fresh)
	for name, got := range map[string]textarea.Styles{
		"fill-in":   single.fillIn.Styles(),
		"note":      single.noteEditor.Styles(),
		"free text": form.questions[len(form.questions)-1].(*FreeText).editor.Styles(),
	} {
		require.Equal(t, want.Cursor.Color, got.Cursor.Color, name)
		require.Equal(t, want.Focused.Text.GetForeground(), got.Focused.Text.GetForeground(), name)
		require.Equal(t, want.Focused.Placeholder.GetForeground(), got.Focused.Placeholder.GetForeground(), name)
	}
	require.NotEqual(t, questionTextareaStyles(new(styles.CharmtonePantera())).Focused.Text.GetForeground(),
		want.Focused.Text.GetForeground(), "the themes must look different")
}

// The gutter bar marks which item the selection is on. A label or
// description long enough to wrap must carry it down every row, or the
// bar breaks into disconnected stubs and one item reads as several.
func TestChoiceBarSurvivesWrapping(t *testing.T) {
	t.Parallel()

	s := styles.CharmtonePantera()
	req := question.Question{
		ID:   "q1",
		Type: question.TypeSingleChoice,
		Text: "Pick one",
		Choices: []question.Choice{
			{
				ID:          "a",
				Label:       "Keep scene grouping, accept the rewinds",
				Description: "Scenes stay whole; 273 groups, avg 3.8 photos",
			},
			{ID: "b", Label: "Groups must be consecutive runs"},
		},
	}

	for _, tc := range []struct {
		name  string
		setUp func(d *SingleChoice)
	}{
		{"keyboard", func(d *SingleChoice) { d.mouseActive = false; d.cursorIdx = 0 }},
		{"hover", func(d *SingleChoice) { d.mouseActive = true; d.hoveredChoice = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			d := NewSingleChoice(&s, req)
			tc.setUp(d)

			lines := d.buildLines(36, "> ", func(int, question.Choice, bool, int) string {
				return "label row one\nlabel row two"
			})

			var rows int
			for _, ln := range lines {
				if ln.choiceIdx != 0 || ansi.Strip(ln.text) == "" {
					continue
				}
				rows++
				require.Contains(t, ansi.Strip(ln.text), "┃",
					"every row of the highlighted choice needs the bar, got %q", ansi.Strip(ln.text))
			}
			require.Greater(t, rows, 2, "the choice must span several rows for this to test anything")
		})
	}
}
