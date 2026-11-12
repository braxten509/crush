package questionform

import (
	"testing"

	"charm.land/bubbles/v2/textarea"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/dialog/internal/kittest"
	"github.com/charmbracelet/crush/internal/ui/styles"
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
