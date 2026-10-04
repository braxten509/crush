package model

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/util"
	"github.com/charmbracelet/crush/internal/workspace"
)

func (m *UI) openTreeDialog() tea.Cmd {
	if m.session == nil {
		return util.ReportInfo("Start a chat before opening its tree.")
	}
	manager, ok := m.com.Workspace.(workspace.TreeManagement)
	if !ok {
		return util.ReportInfo("Session trees currently need a local workspace.")
	}
	entries, err := manager.Tree(context.Background(), m.session.ID)
	if err != nil {
		return util.ReportError(err)
	}
	m.dialog.OpenDialog(dialog.NewTree(m.com, entries))
	return nil
}
func (m *UI) jumpTree(target string) tea.Cmd {
	if m.session == nil {
		return nil
	}
	if m.remote != nil || m.remoteStarting {
		return util.ReportInfo("Turn off phone sharing before switching branches.")
	}
	if m.sessionLoad.active || len(m.pendingPrompts) > 0 {
		return util.ReportInfo("Wait for pending prompts before switching branches.")
	}
	manager, ok := m.com.Workspace.(workspace.TreeManagement)
	if !ok {
		return util.ReportInfo("Session trees currently need a local workspace.")
	}
	if err := manager.SwitchTree(context.Background(), m.session.ID, target); err != nil {
		return util.ReportError(err)
	}
	m.dialog.CloseDialog(dialog.TreeID)
	return tea.Batch(m.resetPlanModeState(), m.loadSession(m.session.ID), util.ReportInfo("Branch switched. Files on disk are unchanged."))
}
func (m *UI) labelTree(action dialog.ActionTreeLabel) tea.Cmd {
	if m.session == nil {
		return nil
	}
	manager, ok := m.com.Workspace.(workspace.TreeManagement)
	if !ok {
		return nil
	}
	if err := manager.LabelTree(context.Background(), m.session.ID, action.MessageID, action.Label); err != nil {
		return util.ReportError(err)
	}
	m.dialog.CloseDialog(dialog.TreeID)
	return m.openTreeDialog()
}
