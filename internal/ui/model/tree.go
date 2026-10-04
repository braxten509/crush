package model

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"errors"
	"fmt"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/util"
	"github.com/charmbracelet/crush/internal/workspace"
	"path/filepath"
	"time"
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

func (m *UI) treeReady() error {
	if m.session == nil {
		return fmt.Errorf("start a chat first")
	}
	if m.remote != nil || m.remoteStarting {
		return fmt.Errorf("turn off phone sharing first")
	}
	if m.sessionLoad.active {
		return fmt.Errorf("wait for the chat to finish loading")
	}
	if m.isAgentBusy() {
		return fmt.Errorf("wait for the reply to finish")
	}
	if len(m.pendingPrompts) > 0 {
		return fmt.Errorf("wait for pending prompts, or recall them first")
	}
	return nil
}
func (m *UI) confirmTree(target, prompt string) tea.Cmd {
	if err := m.treeReady(); err != nil {
		return util.ReportError(err)
	}
	if d, ok := m.dialog.Dialog(dialog.TreeID).(*dialog.Tree); ok {
		d.Confirm(target, prompt)
	}
	return nil
}
func (m *UI) editTree(target string) tea.Cmd {
	if err := m.treeReady(); err != nil {
		return util.ReportError(err)
	}
	manager, ok := m.com.Workspace.(workspace.TreeManagement)
	if !ok {
		return util.ReportInfo("Session trees need a local workspace.")
	}
	msg, err := manager.TreeMessage(context.Background(), target)
	if err != nil {
		return util.ReportInfo("Choose a user prompt to edit.")
	}
	if msg.Role != message.User {
		return util.ReportInfo("Choose a user prompt to edit.")
	}

	entries, err := manager.Tree(context.Background(), m.session.ID)
	if err != nil {
		return util.ReportError(err)
	}
	for _, entry := range entries {
		if entry.MessageID == target {
			if cmd := m.confirmTree(entry.ParentID, msg.Content().Text); cmd != nil {
				return cmd
			}
			if d, ok := m.dialog.Dialog(dialog.TreeID).(*dialog.Tree); ok {
				d.EditMessageID = target
			}
			return nil
		}
	}
	return util.ReportInfo("That prompt is no longer available.")
}

type treeFinishedMsg struct {
	sessionID, prompt string
	editID            string
	err               error
}

func (m *UI) navigateTree(action dialog.ActionTreeNavigate) tea.Cmd {
	if err := m.treeReady(); err != nil {
		return util.ReportError(err)
	}
	manager, ok := m.com.Workspace.(workspace.TreeManagement)
	if !ok {
		return util.ReportInfo("Session trees need a local workspace.")
	}
	id := m.session.ID
	editID := ""
	if d, ok := m.dialog.Dialog(dialog.TreeID).(*dialog.Tree); ok {
		editID = d.EditMessageID
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	if d, ok := m.dialog.Dialog(dialog.TreeID).(*dialog.Tree); ok {
		d.Working(cancel)
	}
	return func() tea.Msg {
		defer cancel()
		err := manager.NavigateTree(ctx, id, action.MessageID, action.Summary)
		return treeFinishedMsg{sessionID: id, prompt: action.Prompt, editID: editID, err: err}
	}
}
func (m *UI) finishTree(result treeFinishedMsg) tea.Cmd {
	if d, ok := m.dialog.Dialog(dialog.TreeID).(*dialog.Tree); ok {
		d.Finish()
	}
	if result.err != nil {
		if errors.Is(result.err, context.Canceled) {
			return util.ReportInfo("Jump canceled. Your old branch is still selected.")
		}
		return util.ReportError(result.err)
	}
	m.dialog.CloseDialog(dialog.TreeID)
	if result.editID != "" {
		m.treeComposer(result.editID, result.prompt)
	}
	return tea.Batch(m.resetPlanModeState(), m.loadSession(result.sessionID), util.ReportInfo("Branch switched. Files on disk are unchanged."))
}
func (m *UI) copyTree(target string, fork bool) tea.Cmd {
	if err := m.treeReady(); err != nil {
		return util.ReportError(err)
	}
	manager, ok := m.com.Workspace.(workspace.TreeManagement)
	if !ok {
		return util.ReportInfo("Session trees need a local workspace.")
	}
	id, prompt, err := manager.CopyTree(context.Background(), m.session.ID, target, fork)
	if err != nil {
		return util.ReportError(err)
	}
	m.dialog.CloseDialog(dialog.TreeID)
	if fork {
		m.treeComposer(target, prompt)
	}
	return tea.Batch(m.resetPlanModeState(), m.loadSession(id), util.ReportInfo("Branch copied to a new chat. Files on disk are unchanged."))
}
func (m *UI) cloneTree() tea.Cmd {
	if err := m.treeReady(); err != nil {
		return util.ReportError(err)
	}
	manager, ok := m.com.Workspace.(workspace.TreeManagement)
	if !ok {
		return util.ReportInfo("Session trees need a local workspace.")
	}
	entries, err := manager.Tree(context.Background(), m.session.ID)
	if err != nil {
		return util.ReportError(err)
	}
	target := ""
	for _, entry := range entries {
		if entry.Active {
			target = entry.MessageID
			break
		}
	}
	return m.copyTree(target, false)
}

func (m *UI) treeComposer(id, prompt string) {
	m.textarea.SetValue(prompt)
	manager, ok := m.com.Workspace.(workspace.TreeManagement)
	if !ok {
		return
	}
	msg, err := manager.TreeMessage(context.Background(), id)
	if err != nil {
		return
	}
	m.attachments.Reset()
	for i, binary := range msg.BinaryContent() {
		name := filepath.Base(binary.Path)
		if name == "." || name == "" {
			name = fmt.Sprintf("attachment-%d", i+1)
		}
		m.attachments.Update(message.Attachment{FilePath: binary.Path, FileName: name, MimeType: binary.MIMEType, Content: binary.Data})
	}
}
