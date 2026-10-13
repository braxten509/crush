package questionform

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/exp/golden"
)

// TestQuestionFormTabBar pins how the tab bar lays out, truncates and
// highlights tabs across widths, the active tab, focus and hover.
func TestQuestionFormTabBar(t *testing.T) {
	t.Parallel()

	sty := styles.CharmtonePantera()
	req := question.Request{Questions: []question.Question{
		{ID: "a", Type: question.TypeFreeText, Text: "What should the project be called?"},
		{ID: "b", Type: question.TypeYesNo, Text: "Ship it?"},
		{ID: "c", Type: question.TypeFreeText, Text: "Which database engine and version should we target in production?"},
	}}

	var b strings.Builder
	for _, width := range []int{120, 70, 40, 24, 12} {
		for _, active := range []int{0, 2, 3} {
			for _, focused := range []bool{true, false} {
				for _, hoverX := range []int{-1, 3, 30} {
					f := NewQuestionForm(&sty, req)
					f.SetFocused(focused)
					f.switchTab(active)
					f.SetHover(hoverX, 1)
					height := f.Height(width)
					scr := uv.NewScreenBuffer(width, height)
					f.Draw(scr, scr.Bounds())
					fmt.Fprintf(&b, "=== width=%d active=%d focused=%v hover=%d\n%s\n\n", width, active, focused, hoverX, scr.Render())
				}
			}
		}
	}
	golden.RequireEqual(t, []byte(b.String()))
}
