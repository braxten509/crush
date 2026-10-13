package model

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/event"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/skills"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/util"
)

// substituteArgs replaces $ARG_NAME placeholders in content with actual values.
func substituteArgs(content string, args map[string]string) string {
	for name, value := range args {
		placeholder := "$" + name
		content = strings.ReplaceAll(content, placeholder, value)
	}
	return content
}

// openDialog opens a dialog by its ID.
func (m *UI) openDialog(id string) tea.Cmd {
	var cmds []tea.Cmd
	switch id {
	case dialog.TreeID:
		return m.openTreeDialog()
	case dialog.ForkID:
		cmd := m.openTreeDialog()
		if d, ok := m.dialog.Dialog(dialog.TreeID).(*dialog.Tree); ok {
			d.SetFork()
		}
		return cmd
	case dialog.SessionsID:
		if cmd := m.openSessionsDialog(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.ModelsID:
		if cmd := m.openModelsDialog(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.CommandsID:
		if cmd := m.openCommandsDialog(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.UsageID:
		return m.openUsageDialog()
	case dialog.AutocompactID:
		return m.openAutocompactDialog()
	case dialog.ReasoningID:
		if cmd := m.openReasoningDialog(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.NotificationsID:
		if cmd := m.openNotificationsDialog(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.SkillsID:
		if m.dialog.ContainsDialog(dialog.SkillsID) {
			m.dialog.BringToFront(dialog.SkillsID)
			break
		}
		manager, ok := m.com.Workspace.(skills.Management)
		if !ok {
			cmds = append(cmds, util.ReportInfo("Skill management is unavailable in this workspace."))
			break
		}
		picker := dialog.NewSkills(m.com, manager)
		m.dialog.OpenDialog(picker)
		cmds = append(cmds, picker.Load())
	case dialog.BackgroundID:
		cmds = append(cmds, m.openBackgroundDialog())
	case dialog.SubAgentsID:
		cmds = append(cmds, m.openSubAgentsDialog())
	case dialog.RemoteID:
		cmds = append(cmds, m.openRemote())
	case dialog.ProjectsID:
		if m.dialog.ContainsDialog(dialog.ProjectsID) {
			m.dialog.BringToFront(dialog.ProjectsID)
			break
		}
		projectsDialog, err := dialog.NewProjects(m.com)
		if err != nil {
			cmds = append(cmds, util.ReportInfo(err.Error()))
			break
		}
		m.dialog.OpenDialog(projectsDialog)
	case dialog.MCPTogglesID:
		if cmd := m.openMCPTogglesDialog(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.KeptSkillsID:
		if cmd := m.openKeptSkillsDialog(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.FilePickerID:
		if cmd := m.openFilesDialog(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.ThemeID:
		m.openThemeDialog()
	case dialog.ThemeNewID:
		m.openThemeNewDialog()
	case dialog.ThemeEditorID:
		m.openThemeEditorDialog("")
	case dialog.QuitID:
		if cmd := m.openQuitDialog(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	default:
		// Unknown dialog
		break
	}
	return tea.Batch(cmds...)
}

// openQuitDialog opens the quit confirmation dialog.
func (m *UI) openQuitDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.QuitID) {
		// Bring to front
		m.dialog.BringToFront(dialog.QuitID)
		return nil
	}

	quitDialog := dialog.NewQuit(m.com)
	m.dialog.OpenDialog(quitDialog)
	return nil
}

// openModelsDialog opens the models dialog.
func (m *UI) openModelsDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.ModelsID) {
		// Bring to front
		m.dialog.BringToFront(dialog.ModelsID)
		return nil
	}

	isOnboarding := m.state == uiOnboarding
	modelsDialog, err := dialog.NewModels(m.com, isOnboarding)
	if err != nil {
		return util.ReportError(err)
	}

	m.dialog.OpenDialog(modelsDialog)

	return nil
}

// openCommandsDialog opens the commands dialog.
func (m *UI) openCommandsDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.CommandsID) {
		// Bring to front
		m.dialog.BringToFront(dialog.CommandsID)
		return nil
	}

	var sessionID string
	hasSession := m.session != nil
	if hasSession {
		sessionID = m.session.ID
	}
	hasTodos := hasSession && hasIncompleteTodos(m.session.Todos)
	hasQueue := m.queue.count > 0

	commands, err := dialog.NewCommands(m.com, sessionID, hasSession, hasTodos, hasQueue, m.customCommands, m.mcpPrompts)
	if err != nil {
		return util.ReportError(err)
	}

	m.dialog.OpenDialog(commands)

	return commands.InitialCmd()
}

// openReasoningDialog opens the reasoning effort dialog.
func (m *UI) openReasoningDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.ReasoningID) {
		m.dialog.BringToFront(dialog.ReasoningID)
		return nil
	}

	reasoningDialog, err := dialog.NewReasoning(m.com)
	if err != nil {
		return util.ReportError(err)
	}

	m.dialog.OpenDialog(reasoningDialog)
	return nil
}

// openNotificationsDialog opens the notification style picker dialog.
func (m *UI) openNotificationsDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.NotificationsID) {
		m.dialog.BringToFront(dialog.NotificationsID)
		return nil
	}

	notificationsDialog := dialog.NewNotifications(m.com)
	m.dialog.OpenDialog(notificationsDialog)
	return nil
}

// openSessionsDialog opens the sessions dialog. If the dialog is already open,
// it brings it to the front. Otherwise, it will list all the sessions and open
// the dialog.
func (m *UI) openSessionsDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.SessionsID) {
		// Bring to front
		m.dialog.BringToFront(dialog.SessionsID)
		return nil
	}

	selectedSessionID := ""
	if m.session != nil {
		selectedSessionID = m.session.ID
	}

	dialog, err := dialog.NewSessions(m.com, selectedSessionID)
	if err != nil {
		return util.ReportError(err)
	}

	m.dialog.OpenDialog(dialog)
	return nil
}

// openFilesDialog opens the file picker dialog.
func (m *UI) openFilesDialog() tea.Cmd {
	if !m.currentModelSupportsImages() {
		return util.ReportWarn("The current model does not support image attachments")
	}
	if m.dialog.ContainsDialog(dialog.FilePickerID) {
		// Bring to front
		m.dialog.BringToFront(dialog.FilePickerID)
		return nil
	}

	filePicker, cmd := dialog.NewFilePicker(m.com)
	filePicker.SetImageCapabilities(&m.caps)
	m.dialog.OpenDialog(filePicker)
	event.FilePickerOpened()

	return cmd
}

// openPermissionsDialog opens the permissions dialog for a permission request.
func (m *UI) openPermissionsDialog(perm permission.PermissionRequest) tea.Cmd {
	// Close any existing permissions dialog first.
	m.dialog.CloseDialog(dialog.PermissionsID)

	// Get diff mode from config.
	var opts []dialog.PermissionsOption
	if diffMode := m.com.Config().Options.TUI.DiffMode; diffMode != "" {
		opts = append(opts, dialog.WithDiffMode(diffMode == "split"))
	}

	permDialog := dialog.NewPermissions(m.com, perm, opts...)
	m.dialog.OpenDialogWithGrace(permDialog)
	return nil
}

// openBatchFormDialog activates a tabbed multi-question form in
// the editor area. Single questions render without tabs or confirm.
func (m *UI) openBatchFormDialog(batch question.Request) tea.Cmd {
	var cmd tea.Cmd
	if m.cliUpdatePromptOpen() {
		// The question takes the editor; the CLI update offer moves to
		// the status bar instead of vanishing.
		cmd = m.movePromptToStatus()
		m.activeInline = nil
	}
	// Close any existing question form first to prevent stacking.
	m.dropQuestionForm()

	form := dialog.NewQuestionForm(m.com.Styles, batch)
	form.SetImageCapabilities(&m.caps)
	form.OnAnswer = func(responses []question.Answer) {
		m.com.Workspace.QuestionAnswer(responses)
	}
	form.OnCancel = func() {
		m.com.Workspace.QuestionCancel()
	}
	m.openInline(form)
	m.updateLayoutAndSize()
	return cmd
}
