package dialog

import (
	"image"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/filehistory"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/stretchr/testify/require"
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
	// Narrow and wide windows draw without a panic.
	for _, size := range []image.Point{{40, 12}, {80, 24}, {120, 40}} {
		screen := uv.NewScreenBuffer(size.X, size.Y)
		d.Draw(screen, image.Rect(0, 0, size.X, size.Y))
	}
}

func TestCheckingFilesStateIsStableAndBlocksActions(t *testing.T) {
	style := styles.CharmtonePantera()
	com := &common.Common{Styles: &style}
	canceled := false
	d := NewFileRestoreBusy(com, "Checking files…", func() { canceled = true })
	require.Nil(t, d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter}))
	require.Nil(t, d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyRight}))
	for _, size := range []image.Point{{40, 12}, {80, 24}, {120, 40}} {
		screen := uv.NewScreenBuffer(size.X, size.Y)
		d.Draw(screen, image.Rect(0, 0, size.X, size.Y))
	}
	require.Equal(t, ActionClose{}, d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEscape}))
	require.True(t, canceled)
	applying := NewFileRestoreBusy(com, "Restoring files…", nil)
	require.Nil(t, applying.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEscape}))
}
