package model

import (
	"image"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/stretchr/testify/require"
)

// A click on a question form runs on release; one that finishes the form
// closes it and hands back the answer, like a direct click used to.
func TestQuestionFormClickRunsOnRelease(t *testing.T) {
	t.Parallel()

	u := newTestUI()
	u.dialog = dialog.NewOverlay()
	var got []question.Answer
	form := dialog.NewQuestionForm(u.com.Styles, question.Request{Questions: []question.Question{{
		ID: "q", Type: question.TypeYesNo, Text: "Ship it?",
	}}})
	form.OnAnswer = func(a []question.Answer) { got = a }
	form.SetFocused(true)
	u.activeInline = form
	u.layout.editor = image.Rect(0, 10, 80, 10+form.Height(80))
	scr := uv.NewScreenBuffer(80, u.layout.editor.Max.Y)
	form.Draw(scr, u.layout.editor)

	// Find "Yes" on screen.
	x, y := -1, -1
	for yy := u.layout.editor.Min.Y; yy < u.layout.editor.Max.Y && x < 0; yy++ {
		for xx := 0; xx < 78; xx++ {
			if scr.CellAt(xx, yy).Content == "Y" && scr.CellAt(xx+1, yy).Content == "e" {
				x, y = xx, yy
				break
			}
		}
	}
	require.GreaterOrEqual(t, x, 0)

	u.Update(tea.MouseClickMsg{X: x, Y: y, Button: uv.MouseLeft})
	require.NotNil(t, u.activeInline, "nothing happens until release")
	require.Nil(t, got)
	u.Update(tea.MouseReleaseMsg{X: x, Y: y, Button: uv.MouseLeft})
	require.Nil(t, u.activeInline, "the form closes")
	require.Len(t, got, 1)
}
