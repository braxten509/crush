package model

import (
	"errors"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/util"
)

func (m *UI) openAutoBackgroundDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.AutoBackgroundID) {
		m.dialog.BringToFront(dialog.AutoBackgroundID)
		return nil
	}
	d, err := dialog.NewAutoBackground(m.com)
	if err != nil {
		return util.ReportError(err)
	}
	m.dialog.OpenDialog(d)
	return nil
}

// setAutoBackground saves the time before a command is moved to the
// background. Commands started after the save follow it; a Claude Code
// chat takes it up at its next turn.
func (m *UI) setAutoBackground(seconds int) tea.Cmd {
	if m.com.Config() == nil {
		return util.ReportError(errors.New("configuration not found"))
	}
	if err := config.ValidateAutoBackgroundSeconds(seconds); err != nil {
		return util.ReportError(err)
	}
	m.dialog.CloseDialog(dialog.AutoBackgroundID)
	ws := m.com.Workspace
	return func() tea.Msg {
		if err := ws.SetConfigField(config.ScopeGlobal, "options.auto_background_seconds", seconds); err != nil {
			return util.ReportError(err)()
		}
		return util.NewInfoMsg(fmt.Sprintf("Commands move to the background after %d seconds", seconds))
	}
}
