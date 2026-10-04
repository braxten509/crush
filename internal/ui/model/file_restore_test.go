package model

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"github.com/charmbracelet/crush/internal/filehistory"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/workspace"
	"github.com/stretchr/testify/require"
	"testing"
)

type restoreWorkspace struct {
	*testWorkspace
	workspace.TreeManagement
	calls int
}

func (w *restoreWorkspace) PreviewFiles(context.Context, string, string, bool) (*filehistory.Plan, error) {
	w.calls++
	return &filehistory.Plan{SessionID: "chat", Entries: []filehistory.Entry{{Path: "file"}}}, nil
}
func (w *restoreWorkspace) PreviewUndoFiles(ctx context.Context, id string) (*filehistory.Plan, error) {
	return w.PreviewFiles(ctx, id, "", false)
}
func (w *restoreWorkspace) UndoFiles(context.Context, *filehistory.Plan) (filehistory.Result, error) {
	w.calls++
	return filehistory.Result{Restored: 1}, nil
}
func (w *restoreWorkspace) CopyTree(context.Context, string, string, bool) (string, string, error) {
	w.calls++
	return "copy", "prompt", nil
}
func (w *restoreWorkspace) NavigateTree(context.Context, string, string, bool) error {
	w.calls++
	return nil
}

func TestFileOperationsRunOnlyInTeaCommands(t *testing.T) {
	for _, operation := range []string{"preview", "undo preview", "fork apply", "navigate apply", "undo apply"} {
		t.Run(operation, func(t *testing.T) {
			ui, base := newPlanUI(t, "chat")
			ws := &restoreWorkspace{testWorkspace: base}
			ui.com.Workspace = ws
			var command tea.Cmd
			plan := &filehistory.Plan{SessionID: "chat", UndoID: "undo"}
			switch operation {
			case "preview":
				_, command = ui.previewFiles("", false, dialog.ActionTreeNavigate{})
			case "undo preview":
				command = ui.previewUndoFiles()
			case "fork apply":
				command = ui.copyTree("target", true, plan)
			case "navigate apply":
				command = ui.navigateTree(dialog.ActionTreeNavigate{}, plan)
			case "undo apply":
				command = ui.chooseFileRestore(dialog.ActionFileRestore{Plan: plan, Restore: true})
			}
			require.NotNil(t, command)
			require.Zero(t, ws.calls, "the update thread must not touch files")
			require.NotNil(t, ui.dialog.Dialog(dialog.FileRestoreID))
			result := command()
			require.Equal(t, 1, ws.calls)
			if preview, ok := result.(filePreviewMsg); ok {
				require.Nil(t, ui.finishFilePreview(preview))
				require.NotSame(t, preview.busy, ui.dialog.Dialog(dialog.FileRestoreID))
			}
		})
	}
}
func TestCanceledPreviewCannotReopenDialog(t *testing.T) {
	ui, base := newPlanUI(t, "chat")
	ui.com.Workspace = &restoreWorkspace{testWorkspace: base}
	_, command := ui.previewFiles("", false, dialog.ActionTreeNavigate{})
	ui.dialog.CloseDialog(dialog.FileRestoreID)
	require.Nil(t, ui.finishFilePreview(command().(filePreviewMsg)))
	require.Nil(t, ui.dialog.Dialog(dialog.FileRestoreID))
}
