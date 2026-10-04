package dialog

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/filehistory"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/stretchr/testify/require"
	"image"
	"strings"
	"testing"
)

func TestFileRestoreChoicesAndNarrowViewport(t *testing.T) {
	style := styles.CharmtonePantera()
	com := &common.Common{Styles: &style}
	plan := &filehistory.Plan{Entries: []filehistory.Entry{{Path: "/long/project/path/file.txt", Action: "restore", Adds: 2, Dels: 3, Conflict: "Changed outside this branch; will skip"}}}
	next := ActionTreeNavigate{MessageID: "target"}
	d := NewFileRestore(com, plan, next)
	require.Equal(t, ActionFileRestore{plan, next, true}, d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter}))
	d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyRight})
	require.Equal(t, ActionFileRestore{plan, next, false}, d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter}))
	d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyRight})
	require.Equal(t, ActionClose{}, d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter}))
	for _, size := range []image.Point{{40, 12}, {80, 24}, {120, 40}} {
		screen := uv.NewScreenBuffer(size.X, size.Y)
		d.Draw(screen, image.Rect(0, 0, size.X, size.Y))
		var rendered strings.Builder
		for y := 0; y < size.Y; y++ {
			for x := 0; x < size.X; x++ {
				cell := screen.CellAt(x, y)
				if cell != nil {
					rendered.WriteString(cell.Content)
				}
			}
			rendered.WriteByte('\n')
		}
		require.Contains(t, rendered.String(), "Restore files")
		require.Contains(t, rendered.String(), "Chat only")
		require.Contains(t, rendered.String(), "Cancel")
	}
}
