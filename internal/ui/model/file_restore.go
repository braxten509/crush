package model

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"github.com/charmbracelet/crush/internal/filehistory"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/util"
	"github.com/charmbracelet/crush/internal/workspace"
	"time"
)

func (m *UI) previewFiles(target string, fork bool, next dialog.Action) (bool, tea.Cmd) {
	manager, ok := m.com.Workspace.(workspace.FileRestoration)
	if !ok {
		return false, nil
	}
	id := m.session.ID
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	busy := dialog.NewFileRestoreBusy(m.com, "Checking files…", cancel)
	m.dialog.OpenDialog(busy)
	return true, func() tea.Msg {
		defer cancel()
		plan, err := manager.PreviewFiles(ctx, id, target, fork)
		return filePreviewMsg{busy, id, plan, next, err}
	}
}
func (m *UI) previewUndoFiles() tea.Cmd {
	if err := m.treeReady(); err != nil {
		return util.ReportError(err)
	}
	manager, ok := m.com.Workspace.(workspace.FileRestoration)
	if !ok {
		return util.ReportInfo("File restore needs a local workspace.")
	}
	id := m.session.ID
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	busy := dialog.NewFileRestoreBusy(m.com, "Checking files…", cancel)
	m.dialog.CloseDialog(dialog.CommandsID)
	m.dialog.OpenDialog(busy)
	return func() tea.Msg {
		defer cancel()
		plan, err := manager.PreviewUndoFiles(ctx, id)
		return filePreviewMsg{busy, id, plan, nil, err}
	}
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
	m.dialog.OpenDialog(dialog.NewFileRestoreBusy(m.com, "Restoring files…", nil))
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		result, err := manager.UndoFiles(ctx, plan)
		return fileUndoMsg{result, err}
	}
}

type filePreviewMsg struct {
	busy      *dialog.FileRestore
	sessionID string
	plan      *filehistory.Plan
	next      dialog.Action
	err       error
}
type fileUndoMsg struct {
	result filehistory.Result
	err    error
}

func (m *UI) finishFilePreview(result filePreviewMsg) tea.Cmd {
	if m.dialog.Dialog(dialog.FileRestoreID) != result.busy {
		return nil
	}
	m.dialog.CloseDialog(dialog.FileRestoreID)
	if m.session == nil || m.session.ID != result.sessionID {
		return nil
	}
	if result.err != nil {
		return util.ReportError(result.err)
	}
	if err := m.treeReady(); err != nil {
		return util.ReportError(err)
	}
	if len(result.plan.Entries) == 0 && result.next != nil {
		switch action := result.next.(type) {
		case dialog.ActionTreeNavigate:
			return m.navigateTree(action, nil)
		case dialog.ActionTreeCopy:
			return m.copyTree(action.MessageID, action.Fork, nil)
		}
	}
	m.dialog.OpenDialog(dialog.NewFileRestore(m.com, result.plan, result.next))
	return nil
}
func (m *UI) finishFileUndo(result fileUndoMsg) tea.Cmd {
	m.dialog.CloseDialog(dialog.FileRestoreID)
	if result.err != nil {
		return util.ReportError(result.err)
	}
	return util.ReportInfo(result.result.Notice())
}
