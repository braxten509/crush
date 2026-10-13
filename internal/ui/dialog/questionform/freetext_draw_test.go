package questionform

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/exp/golden"
)

// TestFreeTextDraw pins the free-text question's drawing, cursor and
// scrolling across sizes, focus and answer lengths.
func TestFreeTextDraw(t *testing.T) {
	t.Parallel()

	sty := styles.CharmtonePantera()
	var b strings.Builder
	for _, answer := range []string{"", "short answer", strings.Repeat("a long answer that wraps ", 12)} {
		for _, focused := range []bool{true, false} {
			for _, size := range [][2]int{{80, 16}, {40, 8}, {30, 5}} {
				d := NewFreeText(&sty, question.Question{
					ID: "q", Type: question.TypeFreeText,
					Text:        "What should the release notes say about this change?",
					Description: "Keep it short.",
				})
				d.SetFocused(focused)
				for _, r := range answer {
					d.HandleKey(tea.KeyPressMsg{Code: r, Text: string(r)})
				}
				scr := uv.NewScreenBuffer(size[0], size[1])
				cur := d.Draw(scr, scr.Bounds())
				fmt.Fprintf(&b, "=== answer=%d focused=%v size=%v cursor=%v\n%s\n\n", len(answer), focused, size, cur, scr.Render())
			}
		}
	}
	golden.RequireEqual(t, []byte(b.String()))
}
