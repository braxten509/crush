package model

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/ui/util"
)

func (m *UI) toggleFastMode() tea.Cmd {
	if m.isAgentBusy() {
		return util.ReportWarn("Agent is busy, please wait...")
	}
	cfg := m.com.Config()
	if cfg == nil {
		return util.ReportError(errors.New("configuration not found"))
	}
	agentCfg, ok := cfg.Agents[config.AgentCoder]
	if !ok {
		return util.ReportError(errors.New("agent configuration not found"))
	}
	provider := cfg.GetProviderForModel(agentCfg.Model)
	model := cfg.GetModelByType(agentCfg.Model)
	if provider == nil || model == nil || !config.SupportsFastMode(*provider, *model) {
		return util.ReportError(errors.New("selected model does not support FAST mode"))
	}
	selected := cfg.Models[agentCfg.Model]
	status := "enabled"
	if selected.ServiceTier == "fast" {
		selected.ServiceTier, status = "default", "disabled"
	} else {
		selected.ServiceTier = "fast"
	}
	return m.updateAgentModelCmd(func() tea.Msg {
		if err := m.com.Workspace.UpdatePreferredModel(config.ScopeGlobal, agentCfg.Model, selected); err != nil {
			return util.ReportError(err)()
		}
		if err := m.com.Workspace.UpdateAgentModel(context.Background()); err != nil {
			return util.ReportError(err)()
		}
		return util.NewInfoMsg("FAST mode " + status)
	})
}
