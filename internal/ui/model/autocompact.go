package model

import (
	"context"
	"errors"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/util"
)

func (m *UI) openAutocompactDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.AutocompactID) {
		m.dialog.BringToFront(dialog.AutocompactID)
		return nil
	}
	d, err := dialog.NewAutocompact(m.com)
	if err != nil {
		return util.ReportError(err)
	}
	m.dialog.OpenDialog(d)
	return nil
}

func (m *UI) setAutocompact(tokens int64) tea.Cmd {
	if m.isAgentBusy() {
		return util.ReportWarn("Agent is busy, please wait before saving fallback compaction")
	}
	cfg := m.com.Config()
	if cfg == nil {
		return util.ReportError(errors.New("configuration not found"))
	}
	if err := config.ValidateAutoCompactTokenLimit(tokens); err != nil {
		return util.ReportError(err)
	}
	m.dialog.CloseDialog(dialog.AutocompactID)
	return m.updateAgentModelCmd(m.saveAutocompactCmd(tokens))
}

func (m *UI) saveAutocompactCmd(tokens int64) tea.Cmd {
	return func() tea.Msg {
		if err := m.com.Workspace.SetConfigField(config.ScopeGlobal, "options.auto_compact_token_limit", tokens); err != nil {
			return util.ReportError(err)()
		}
		if err := m.com.Workspace.UpdateAgentModel(context.Background()); err != nil {
			return util.ReportError(err)()
		}
		return util.NewInfoMsg(fmt.Sprintf("Fallback compaction set to %d tokens", tokens))
	}
}
