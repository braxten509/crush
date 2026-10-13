package model

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/util"
)

// handleSessionMsg handles the results of session, history, command and
// update loading.
// ok is false for any other message.
func (m *UI) handleSessionMsg(msg tea.Msg) (tea.Cmd, bool) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case filePreviewMsg:
		cmds = append(cmds, m.finishFilePreview(msg))
	case fileUndoMsg:
		cmds = append(cmds, m.finishFileUndo(msg))
	case treeFinishedMsg:
		cmds = append(cmds, m.finishTree(msg))
	case loadSessionMsg, sessionLoadFailedMsg, sessionLoadTickMsg, openSessionMsg:
		cmds = append(cmds, m.handleSessionLoadMsg(msg)...)

	case sessionFilesUpdatesMsg:
		m.sessionFiles = msg.sessionFiles
		var paths []string
		for _, f := range msg.sessionFiles {
			paths = append(paths, f.LatestVersion.Path)
		}
		cmds = append(cmds, m.startLSPs(paths))

	case keptSkillsListMsg:
		if msg.err != nil {
			cmds = append(cmds, util.ReportError(msg.err))
			break
		}
		if !m.dialog.ContainsDialog(dialog.KeptSkillsID) {
			m.dialog.OpenDialog(dialog.NewKeptSkills(m.com, m.keptSkillItems(msg.entries)))
		}
	case keptSkillsChangedMsg:
		if msg.err != nil {
			cmds = append(cmds, util.ReportError(msg.err))
			break
		}
		if m.session != nil && m.session.ID == msg.session.ID {
			m.session.KeptSkills = msg.session.KeptSkills
			m.syncKeptSkillsDialog()
			m.frame.invalidate()
		}
	case userCommandsLoadedMsg:
		m.customCommands = msg.Commands
		dia := m.dialog.Dialog(dialog.CommandsID)
		if dia == nil {
			break
		}

		commands, ok := dia.(*dialog.Commands)
		if ok {
			commands.SetCustomCommands(m.customCommands)
		}

	case mcpPromptsLoadedMsg:
		m.mcpPrompts = msg.Prompts
		dia := m.dialog.Dialog(dialog.CommandsID)
		if dia == nil {
			break
		}

		commands, ok := dia.(*dialog.Commands)
		if ok {
			commands.SetMCPPrompts(m.mcpPrompts)
		}

	case promptHistoryLoadedMsg:
		m.promptHistory.messages = msg.messages
		m.promptHistory.index = -1
		m.promptHistory.draft = ""
	case reviewLoadedMsg:
		cmds = append(cmds, m.applyReview(msg))
	case cliUpdatesMsg:
		cmds = append(cmds, m.handleCLIUpdates(msg))
	case selfUpdateMsg:
		cmds = append(cmds, m.handleSelfUpdate(msg))
	case selfUpdateBuiltMsg:
		cmds = append(cmds, m.handleSelfUpdateBuilt(msg))
	case selfUpdateInstalledMsg:
		cmds = append(cmds, m.handleSelfUpdateInstalled(msg))
	case cliUpdatesInstalledMsg:
		cmds = append(cmds, m.handleCLIUpdatesInstalled(msg))
	default:
		return nil, false
	}
	return tea.Batch(cmds...), true
}
