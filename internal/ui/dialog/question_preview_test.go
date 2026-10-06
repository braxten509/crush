package dialog

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/stretchr/testify/require"
)

// writeSketch writes a w×h PNG and returns its path.
func writeSketch(t *testing.T, name string, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: 0x12, G: 0x12, B: 0x2a, A: 0xff})
		}
	}
	path := filepath.Join(t.TempDir(), name)
	f, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, png.Encode(f, img))
	require.NoError(t, f.Close())
	return path
}

func previewForm(t *testing.T, kitty bool) (*QuestionForm, string) {
	t.Helper()
	sty := styles.CharmtonePantera()
	pic := writeSketch(t, "a.png", 480, 220)
	form := NewQuestionForm(&sty, question.Request{Questions: []question.Question{{
		ID: "look", Type: question.TypeSingleChoice, Text: "Which look?", Description: "Pick one.",
		Choices: []question.Choice{
			{ID: "a", Label: "Arcade card", Image: pic},
			{ID: "b", Label: "Normal"},
		},
	}}})
	p := newPreviewer(nil)
	p.kitty = kitty
	p.cell.Width, p.cell.Height = 9, 19
	for _, q := range form.questions {
		if h, ok := q.(previewHolder); ok {
			h.setPreviewer(p)
		}
	}
	form.SetFocused(true)
	return form, pic
}

func drawForm(form *QuestionForm, width int) uv.ScreenBuffer {
	h := form.Height(width)
	scr := uv.NewScreenBuffer(width, h)
	form.Draw(scr, image.Rect(0, 0, width, h))
	return scr
}

// With Kitty pictures, the choice under the cursor gets its sketch placed
// over the blank rows under the list, in the text column, keeping its shape.
func TestPreviewPlacedUnderChoices(t *testing.T) {
	t.Parallel()
	form, pic := previewForm(t, true)
	require.True(t, form.PreviewWaiting(), "not drawn yet")
	scr := drawForm(form, 100)
	placed, _ := form.PreviewState()
	require.NotNil(t, placed)
	require.Equal(t, pic, placed.Path)
	require.False(t, form.PreviewWaiting())
	require.LessOrEqual(t, placed.Rows, previewRows)
	// Flush with the choices' text, not centered.
	labelCol := -1
	for y := range scr.Height() {
		var row strings.Builder
		for x := range scr.Width() {
			if c := scr.CellAt(x, y); c != nil {
				row.WriteString(c.Content)
			}
		}
		if i := strings.Index(row.String(), "Arcade card"); i >= 0 {
			labelCol = utf8.RuneCountInString(row.String()[:i])
		}
	}
	require.Equal(t, labelCol, placed.X, "picture starts where the choice text does")
	require.LessOrEqual(t, placed.X+placed.Cols, 100)
	// Its cells are blank, below every choice label.
	for y := placed.Y; y < placed.Y+placed.Rows; y++ {
		for x := placed.X; x < placed.X+placed.Cols; x++ {
			c := scr.CellAt(x, y)
			require.True(t, c == nil || strings.TrimSpace(c.Content) == "", "cell (%d,%d) under the picture is blank", x, y)
		}
	}
	labelRow := -1
	for y := range scr.Height() {
		var row strings.Builder
		for x := range scr.Width() {
			if c := scr.CellAt(x, y); c != nil {
				row.WriteString(c.Content)
			}
		}
		if strings.Contains(row.String(), "Normal") {
			labelRow = y
		}
	}
	require.Greater(t, placed.Y, labelRow, "picture sits under the choices")
}

// A choice without a sketch removes the picture and says so; the area
// keeps its height so the form doesn't shift.
func TestPreviewNoPictureChoice(t *testing.T) {
	t.Parallel()
	form, _ := previewForm(t, true)
	before := form.Height(100)
	drawForm(form, 100)
	sc := form.questions[0].(*SingleChoice)
	sc.moveDown()
	scr := drawForm(form, 100)
	placed, _ := form.PreviewState()
	require.Nil(t, placed)
	require.Equal(t, before, form.Height(100))
	var all strings.Builder
	for y := range scr.Height() {
		for x := range scr.Width() {
			if c := scr.CellAt(x, y); c != nil {
				all.WriteString(c.Content)
			}
		}
	}
	require.Contains(t, all.String(), "No picture for this choice.")
}

// Without Kitty pictures the sketch is drawn with colored blocks once loaded.
func TestPreviewBlocksFallback(t *testing.T) {
	t.Parallel()
	form, _ := previewForm(t, false)
	drawForm(form, 100)
	placed, load := form.PreviewState()
	require.Nil(t, placed)
	require.NotNil(t, load)
	_, ok := load().(PreviewReadyMsg)
	require.True(t, ok)
	_, again := form.PreviewState()
	require.Nil(t, again, "loaded once")
}

// Folding the form away takes the picture down.
func TestPreviewClearedWhenCollapsed(t *testing.T) {
	t.Parallel()
	form, _ := previewForm(t, true)
	drawForm(form, 100)
	placed, _ := form.PreviewState()
	require.NotNil(t, placed)
	form.DrawCollapsed(uv.NewScreenBuffer(100, 1), image.Rect(0, 0, 100, 1))
	placed, _ = form.PreviewState()
	require.Nil(t, placed)
}
