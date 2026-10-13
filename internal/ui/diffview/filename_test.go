package diffview_test

import (
	"fmt"
	"testing"

	"github.com/charmbracelet/crush/internal/ui/diffview"
	"github.com/charmbracelet/x/exp/golden"
)

// TestDiffViewFileName pins the file name header, which is drawn above the
// first hunk and scrolls and clips like any other line.
func TestDiffViewFileName(t *testing.T) {
	t.Parallel()

	for layoutName, layoutFunc := range LayoutFuncs {
		t.Run(layoutName, func(t *testing.T) {
			t.Parallel()
			for _, lineNumbers := range []bool{true, false} {
				for _, yOffset := range []int{0, 1, 2} {
					for _, height := range []int{0, 4} {
						t.Run(fmt.Sprintf("Numbers%v/YOffset%d/Height%d", lineNumbers, yOffset, height), func(t *testing.T) {
							t.Parallel()
							dv := diffview.New().
								Before("main.go", TestMultipleHunksBefore).
								After("main.go", TestMultipleHunksAfter).
								FileName("internal/very/long/path/to/main.go").
								LineNumbers(lineNumbers).
								YOffset(yOffset).
								Height(height).
								Width(60).
								Style(diffview.DefaultDarkStyle())
							dv = layoutFunc(dv)
							golden.RequireEqual(t, []byte(dv.String()))
						})
					}
				}
			}
		})
	}
}
