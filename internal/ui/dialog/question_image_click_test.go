package dialog

import (
	"github.com/stretchr/testify/require"
	"net/url"
	"testing"
)

func TestQuestionPictureOpensOnlyOnClick(t *testing.T) {
	for _, kitty := range []bool{false, true} {
		for _, drag := range []bool{false, true} {
			form, path := previewForm(t, kitty)
			target := (&url.URL{Scheme: "file", Path: path}).String()
			screen := drawForm(form, 100)
			x, y := -1, -1
			for row := range screen.Height() {
				for col := range screen.Width() {
					if c := screen.CellAt(col, row); c != nil && c.Link.URL == target {
						x, y = col, row
						break
					}
				}
				if x >= 0 {
					break
				}
			}
			require.NotEqual(t, -1, x)
			require.True(t, form.HandleMouseDown(x, y))
			if drag {
				form.HandleMouseDrag(x+2, y)
				form.HandleMouseDrag(x, y)
			}
			handled, cmd := form.HandleMouseRelease(x, y)
			require.True(t, handled)
			if drag {
				if cmd != nil {
					_, opened := cmd().(OpenLinkMsg)
					require.False(t, opened)
				}
			} else {
				require.NotNil(t, cmd)
				require.Equal(t, OpenLinkMsg{URL: target}, cmd())
			}
			require.False(t, form.TakeReleaseDone(), "opening a picture must not submit an answer")
		}
	}
}
