package model

import (
	"cmp"
	"errors"
	"os"

	"github.com/charmbracelet/crush/internal/ui/dialog/mcpservers"
	"github.com/charmbracelet/crush/internal/ui/dialog/remotecontrol"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/home"
	"github.com/charmbracelet/crush/internal/projects"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	fimage "github.com/charmbracelet/crush/internal/ui/image"
	"github.com/charmbracelet/crush/internal/ui/util"
)

// handleDialogMsg gives a message to the front dialog and handles the
// action it answers with.
func (m *UI) handleDialogMsg(msg tea.Msg) tea.Cmd {
	return m.handleDialogAction(m.dialog.Update(msg))
}

// handleDialogAction handles a dialog's action, by concern.
func (m *UI) handleDialogAction(action dialog.Action) tea.Cmd {
	if action == nil {
		return nil
	}
	handlers := [...]func(dialog.Action) (tea.Cmd, bool){
		m.handleThemeAction,
		m.handlePaletteToggle,
		m.handlePaletteAction,
		m.handleSessionDialogAction,
		m.handleTreeAction,
		m.handleRemoteDialogAction,
		m.handleModelAction,
		m.handlePromptAction,
	}
	for _, handle := range handlers {
		if cmd, ok := handle(action); ok {
			return cmd
		}
	}
	return m.handleGenericDialogAction(action)
}

// handleGenericDialogAction handles the actions any dialog may answer with:
// closing, running a command, quitting. Anything else is sent back to the
// program as a message.
func (m *UI) handleGenericDialogAction(action dialog.Action) tea.Cmd {
	var cmds []tea.Cmd
	isOnboarding := m.state == uiOnboarding

	switch msg := action.(type) {
	case dialog.ActionClose:
		if isOnboarding && m.dialog.ContainsDialog(dialog.ModelsID) {
			break
		}

		if m.dialog.ContainsDialog(dialog.FilePickerID) {
			defer fimage.ResetCache()
		}

		m.dialog.CloseFrontDialog()

		if isOnboarding {
			if cmd := m.openModelsDialog(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}

		if m.focus == uiFocusEditor {
			cmds = append(cmds, m.textarea.Focus())
		}
	case dialog.ActionCloseOAuth:
		// Same as ActionClose, but with a cleanup command that cancels
		// the in-flight authorization before the dialog goes away.
		m.dialog.CloseFrontDialog()

		if msg.Cmd != nil {
			cmds = append(cmds, msg.Cmd)
		}

		if isOnboarding {
			if cmd := m.openModelsDialog(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}

		if m.focus == uiFocusEditor {
			cmds = append(cmds, m.textarea.Focus())
		}
	case dialog.ActionCmd:
		if msg.Cmd != nil {
			cmds = append(cmds, msg.Cmd)
		}
	case dialog.ActionSkillsChanged:
		cmds = append(cmds, m.loadCustomCommands())

	case dialog.ActionQuit:
		cmds = append(cmds, tea.Quit)
	default:
		cmds = append(cmds, util.CmdHandler(msg))
	}
	return tea.Batch(cmds...)
}

// handleSessionDialogAction handles the session actions: switching, starting
// and summarizing chats, usage, sub-agents and file restores.
// ok is false for any other action.
func (m *UI) handleSessionDialogAction(action dialog.Action) (cmd tea.Cmd, ok bool) {
	var cmds []tea.Cmd
	switch msg := action.(type) {
	case dialog.ActionRefreshUsage:
		if d, ok := m.dialog.Dialog(dialog.UsageID).(*dialog.Usage); ok {
			cmds = append(cmds, m.refreshUsage(d))
		}
	case dialog.ActionViewSubAgent:
		cmds = append(cmds, m.openSubagentView(msg.Task))
	case dialog.ActionUndoFiles:
		cmds = append(cmds, m.previewUndoFiles())
	case dialog.ActionFileRestore:
		cmds = append(cmds, m.chooseFileRestore(msg))
	case dialog.ActionSelectSession:
		m.dialog.CloseDialog(dialog.SessionsID)
		cmds = append(cmds, m.openSession(msg.Session))

	case dialog.ActionNewSession:
		if m.isAgentBusy() {
			cmds = append(cmds, util.ReportWarn("Agent is busy, please wait before starting a new session..."))
			break
		}
		if cmd := m.newSession(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		m.dialog.CloseDialog(dialog.CommandsID)
	case dialog.ActionSummarize:
		if m.isAgentBusy() {
			cmds = append(cmds, util.ReportWarn("Agent is busy, please wait before summarizing session..."))
			break
		}
		cmds = append(cmds, m.summarizeSession(msg.SessionID))
		m.dialog.CloseDialog(dialog.CommandsID)
	case dialog.ActionInitializeProject:
		if m.isAgentBusy() {
			cmds = append(cmds, util.ReportWarn("Agent is busy, please wait before summarizing session..."))
			break
		}
		cmds = append(cmds, m.initializeProject())
		m.dialog.CloseDialog(dialog.CommandsID)

	default:
		return nil, false
	}
	return tea.Batch(cmds...), true
}

// handleTreeAction handles the conversation tree dialog's actions.
// ok is false for any other action.
func (m *UI) handleTreeAction(action dialog.Action) (cmd tea.Cmd, ok bool) {
	var cmds []tea.Cmd
	switch msg := action.(type) {
	case dialog.ActionTreeJump:
		cmds = append(cmds, m.confirmTree(msg.MessageID, ""))
	case dialog.ActionTreeEdit:
		cmds = append(cmds, m.editTree(msg.MessageID))
	case dialog.ActionTreeNavigate:
		cmds = append(cmds, m.navigateTree(msg))
	case dialog.ActionTreeCopy:
		cmds = append(cmds, m.copyTree(msg.MessageID, msg.Fork))
	case dialog.ActionTreeClone:
		m.dialog.CloseDialog(dialog.CommandsID)
		cmds = append(cmds, m.cloneTree())
	case dialog.ActionTreeLabel:
		cmds = append(cmds, m.labelTree(msg))
	default:
		return nil, false
	}
	return tea.Batch(cmds...), true
}

// handleRemoteDialogAction handles the Remote Control dialogs' actions.
// ok is false for any other action.
func (m *UI) handleRemoteDialogAction(action dialog.Action) (cmd tea.Cmd, ok bool) {
	var cmds []tea.Cmd
	switch msg := action.(type) {
	case remotecontrol.ActionRemoteOff:
		cmds = append(cmds, m.stopRemote())
	case remotecontrol.ActionRemoteSetup:
		cmds = append(cmds, m.runRemoteSetup(msg.Action))
	case remotecontrol.ActionRemoteSetupClose:
		m.closeRemoteSetup()
	default:
		return nil, false
	}
	return tea.Batch(cmds...), true
}

// handleModelAction handles model, reasoning, sign-in method and permission
// choices.
// ok is false for any other action.
func (m *UI) handleModelAction(action dialog.Action) (cmd tea.Cmd, ok bool) {
	var cmds []tea.Cmd
	switch msg := action.(type) {
	case dialog.ActionSelectAuthMethod:
		m.dialog.CloseDialog(dialog.AuthMethodID)
		if cmd := m.openAuthenticationDialogWithMethod(msg.Provider, msg.Model, msg.ModelType, msg.UseOAuth); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.ActionSelectModel:
		if cmd := m.handleSelectModel(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.ActionSelectReasoningEffort:
		if m.isAgentBusy() {
			cmds = append(cmds, util.ReportWarn("Agent is busy, please wait..."))
			break
		}
		// The picker closes once the effort was saved, and stays open
		// when saving failed.
		cmd, err := m.setReasoningEffort(msg.Effort, msg.Ultracode, func(err error) tea.Cmd {
			if err != nil {
				return util.ReportError(err)
			}
			m.dialog.CloseDialog(dialog.ReasoningID)
			return nil
		})
		if err != nil {
			cmds = append(cmds, util.ReportError(err))
			break
		}
		cmds = append(cmds, cmd)
	case dialog.ActionPermissionResponse:
		m.dialog.CloseDialog(dialog.PermissionsID)
		switch msg.Action {
		case dialog.PermissionAllow:
			m.com.Workspace.PermissionGrant(msg.Permission)
		case dialog.PermissionAllowForSession:
			m.com.Workspace.PermissionGrantPersistent(msg.Permission)
		case dialog.PermissionDeny:
			m.com.Workspace.PermissionDeny(msg.Permission)
		}

	default:
		return nil, false
	}
	return tea.Batch(cmds...), true
}

// handlePromptAction handles the actions that put something into the
// prompt or send one: files, custom commands, skills and MCP prompts.
// ok is false for any other action.
func (m *UI) handlePromptAction(action dialog.Action) (cmd tea.Cmd, ok bool) {
	var cmds []tea.Cmd
	switch msg := action.(type) {
	case dialog.ActionFilePickerSelected:
		// The picker closes on the Update goroutine once the selection's
		// own command has delivered its message.
		cmds = append(cmds, tea.Sequence(
			msg.Cmd(),
			util.CmdHandler(filePickerDoneMsg{}),
		))

	case dialog.ActionRunCustomCommand:
		if len(msg.Arguments) > 0 && msg.Args == nil {
			m.dialog.CloseFrontDialog()
			argsDialog := dialog.NewArguments(
				m.com,
				"Custom Command Arguments",
				"",
				msg.Arguments,
				msg, // Pass the action as the result
			)
			m.dialog.OpenDialog(argsDialog)
			break
		}
		content := msg.Content
		if msg.Args != nil {
			content = substituteArgs(content, msg.Args)
		}
		// If this is a skill command, format it using the skill's FormatInvocation method
		if msg.Skill != nil {
			content = msg.Skill.FormatInvocation()
		}
		cmds = append(cmds, m.sendMessage(content))
		m.dialog.CloseFrontDialog()
	case dialog.ActionAttachSkill:
		m.dialog.CloseFrontDialog()
		cmds = append(cmds, m.attachSkill(msg.ID, msg.Name))
	case dialog.ActionToggleKeptSkill:
		cmds = append(cmds, m.setKeptSkill(session.KeptSkill{ID: msg.ID, Name: msg.Name}, msg.On))
	case dialog.ActionRunMCPPrompt:
		if len(msg.Arguments) > 0 && msg.Args == nil {
			m.dialog.CloseFrontDialog()
			title := cmp.Or(msg.Title, "MCP Prompt Arguments")
			argsDialog := dialog.NewArguments(
				m.com,
				title,
				msg.Description,
				msg.Arguments,
				msg, // Pass the action as the result
			)
			m.dialog.OpenDialog(argsDialog)
			break
		}
		cmds = append(cmds, m.runMCPPrompt(msg.ClientID, msg.PromptID, msg.Args))
	default:
		return nil, false
	}
	return tea.Batch(cmds...), true
}

// handlePaletteAction handles the command palette's commands that are not
// toggles, and the settings dialogs' choices.
// ok is false for any other action.
func (m *UI) handlePaletteAction(action dialog.Action) (cmd tea.Cmd, ok bool) {
	var cmds []tea.Cmd
	switch msg := action.(type) {
	case dialog.ActionOpenDialog:
		m.dialog.CloseDialog(dialog.CommandsID)
		if cmd := m.openDialog(msg.DialogID); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case dialog.ActionSelectNotificationStyle:
		cmds = append(cmds, m.selectNotificationStyle(msg.Style))
		m.dialog.CloseDialog(dialog.NotificationsID)
	case dialog.ActionExternalEditor:
		if m.isAgentBusy() {
			cmds = append(cmds, util.ReportWarn("Agent is working, please wait..."))
			break
		}
		editorValue := m.textarea.Value()
		if m.bangMode {
			editorValue = "!" + editorValue
		}
		cmds = append(cmds, m.openEditor(editorValue))
		m.dialog.CloseDialog(dialog.CommandsID)
	case dialog.ActionSetAutocompact:
		cmds = append(cmds, m.setAutocompact(msg.Tokens))
	case dialog.ActionCheckCLIUpdates:
		m.dialog.CloseDialog(dialog.CommandsID)
		cmds = append(cmds, m.checkCLIUpdates(true))
	case dialog.ActionSelfUpdate:
		m.dialog.CloseDialog(dialog.CommandsID)
		cmds = append(cmds, m.checkSelfUpdate(true))
	case dialog.ActionCustomizeComposer:
		m.dialog.CloseDialog(dialog.CommandsID)
		m.openComposerFooterForm()
	case dialog.ActionToggleMouseSupport:
		cfg := m.com.Config()
		if cfg == nil {
			cmds = append(cmds, util.ReportError(errors.New("configuration not found")))
			break
		}
		// Flip the field on the main update path so it never races with
		// View() reading m.mouseEnabled from a background command's
		// goroutine; only the (possibly slow) config write is deferred.
		mouseEnabled := cfg.Options == nil || cfg.Options.TUI.Mouse == nil || *cfg.Options.TUI.Mouse
		newValue := !mouseEnabled
		m.mouseEnabled = newValue
		cmds = append(cmds, m.writeConfigField("options.tui.mouse", newValue, func(err error) tea.Cmd {
			if err != nil {
				return util.ReportError(err)
			}
			status := "disabled"
			if newValue {
				status = "enabled"
			}
			return util.ReportInfo("Mouse support " + status)
		}))
		m.dialog.CloseDialog(dialog.CommandsID)
	case dialog.ActionEnableDockerMCP:
		m.dialog.CloseDialog(dialog.CommandsID)
		cmds = append(cmds, enableDockerMCP(m.com.Workspace))
	case dialog.ActionDisableDockerMCP:
		m.dialog.CloseDialog(dialog.CommandsID)
		cmds = append(cmds, disableDockerMCP(m.com.Workspace))
	case mcpservers.ActionToggleMCP:
		cmds = append(cmds, m.applyMCPToggle(msg))
	case dialog.ActionSaveProject:
		m.dialog.CloseDialog(dialog.CommandsID)
		dir := m.com.Workspace.WorkingDir()
		cmds = append(cmds, func() tea.Msg {
			if err := projects.MarkSaved(dir); err != nil {
				return util.ReportError(err)()
			}
			return util.NewInfoMsg("Saved project " + home.Short(dir))
		})
	case dialog.ActionOpenProject:
		if msg.Path == m.com.Workspace.WorkingDir() {
			m.dialog.CloseDialog(dialog.ProjectsID)
			cmds = append(cmds, util.ReportInfo("Already in this project"))
			break
		}
		if m.isAgentBusy() {
			cmds = append(cmds, util.ReportWarn("Agent is busy, please wait before switching projects..."))
			break
		}
		if _, err := os.Stat(msg.Path); err != nil {
			cmds = append(cmds, util.ReportError(err))
			break
		}
		m.relaunchDir = msg.Path
		cmds = append(cmds, tea.Quit)

	default:
		return nil, false
	}
	return tea.Batch(cmds...), true
}

// handlePaletteToggle handles the command palette's toggles, each of which
// runs and then closes the palette. ok is false for any other action.
func (m *UI) handlePaletteToggle(action dialog.Action) (cmd tea.Cmd, ok bool) {
	var toggle func() tea.Cmd
	switch action.(type) {
	case dialog.ActionToggleYoloMode:
		toggle = m.toggleYoloCommand
	case dialog.ActionToggleHelp:
		toggle = func() tea.Cmd {
			m.status.ToggleHelp()
			return nil
		}
	case dialog.ActionToggleCompactMode:
		toggle = m.toggleCompactMode
	case dialog.ActionTogglePills:
		toggle = m.togglePillsExpanded
	case dialog.ActionToggleThinking:
		toggle = m.toggleThinking
	case dialog.ActionToggleFastMode:
		toggle = m.toggleFastMode
	case dialog.ActionToggleUltracode:
		toggle = m.toggleUltracode
	case dialog.ActionToggleTransparentBackground:
		toggle = m.toggleTransparentBackground
	case dialog.ActionToggleAgentCPUs:
		toggle = m.toggleAgentCPUs
	case dialog.ActionToggleComposerFocusOnly:
		toggle = m.toggleComposerFocusOnly
	default:
		return nil, false
	}
	cmd = toggle()
	m.dialog.CloseDialog(dialog.CommandsID)
	return cmd, true
}
