package model

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/util"
)

// openSubAgentInstructionsDialog opens the editor on the saved sub-agent
// instructions.
func (m *UI) openSubAgentInstructionsDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.SubAgentInstructionsID) {
		m.dialog.BringToFront(dialog.SubAgentInstructionsID)
		return nil
	}
	m.dialog.OpenDialog(dialog.NewSubAgentInstructions(m.com, config.LoadSubAgentInstructions()))
	return nil
}

// saveSubAgentInstructionsCmd saves the edited instructions; every chat's
// agent gets them with its next message.
func saveSubAgentInstructionsCmd(text string) tea.Cmd {
	return func() tea.Msg {
		if err := config.SaveSubAgentInstructions(text); err != nil {
			return util.ReportError(err)()
		}
		return util.NewInfoMsg("Sub-agent instructions saved")
	}
}
