package model

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"github.com/charmbracelet/crush/internal/filehistory"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/util"
	"github.com/charmbracelet/crush/internal/workspace"
)

func (m *UI) previewFiles(target string, fork bool, next dialog.Action) (bool, tea.Cmd) {
	manager, ok := m.com.Workspace.(workspace.FileRestoration)
	if !ok {
		return false, nil
	}
	plan, err := manager.PreviewFiles(context.Background(), m.session.ID, target, fork)
	if err != nil {
		return true, util.ReportError(err)
	}
	if len(plan.Entries) == 0 {
		return false, nil
	}
	m.dialog.OpenDialog(dialog.NewFileRestore(m.com, plan, next))
	return true, nil
}
func (m *UI) previewUndoFiles() tea.Cmd {
	if err := m.treeReady(); err != nil {
		return util.ReportError(err)
	}
	manager, ok := m.com.Workspace.(workspace.FileRestoration)
	if !ok {
		return util.ReportInfo("File restore needs a local workspace.")
	}
	plan, err := manager.PreviewUndoFiles(context.Background(), m.session.ID)
	if err != nil {
		return util.ReportError(err)
	}
	m.dialog.CloseDialog(dialog.CommandsID)
	m.dialog.OpenDialog(dialog.NewFileRestore(m.com, plan, nil))
	return nil
}
func (m *UI) chooseFileRestore(action dialog.ActionFileRestore) tea.Cmd {
	if err := m.treeReady(); err != nil {
		return util.ReportError(err)
	}
	if action.Plan.SessionID != m.session.ID {
		return util.ReportInfo("The chat changed. Open the file preview again.")
	}
	m.dialog.CloseDialog(dialog.FileRestoreID)
	var plan *filehistory.Plan
	if action.Restore {
		plan = action.Plan
	}
	switch next := action.Next.(type) {
	case dialog.ActionTreeNavigate:
		return m.navigateTree(next, plan)
	case dialog.ActionTreeCopy:
		return m.copyTree(next.MessageID, next.Fork, plan)
	}
	if plan == nil {
		return nil
	}
	manager, ok := m.com.Workspace.(workspace.FileRestoration)
	if !ok {
		return util.ReportInfo("File restore needs a local workspace.")
	}
	return func() tea.Msg {
		result, err := manager.UndoFiles(context.Background(), plan)
		if err != nil {
			return util.ReportError(err)()
		}
		return util.ReportInfo(result.Notice())()
	}
}
