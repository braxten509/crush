package model

import (
	"errors"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/ui/util"
)

// toggleFastMode flips FAST mode on the shown chat's model.
func (m *UI) toggleFastMode() tea.Cmd {
	if m.isAgentBusy() {
		return util.ReportWarn("Agent is busy, please wait...")
	}
	selected, provider, model, ok := m.shownModelInfo()
	if !ok {
		return util.ReportError(errors.New("no model is selected"))
	}
	if provider == nil || model == nil || !config.SupportsFastMode(*provider, *model) {
		return util.ReportError(errors.New("selected model does not support FAST mode"))
	}
	status := "enabled"
	if selected.ServiceTier == "fast" {
		selected.ServiceTier, status = "default", "disabled"
	} else {
		selected.ServiceTier = "fast"
	}
	return m.saveShownModel(selected, func(err error) tea.Cmd {
		if err != nil {
			return util.ReportError(err)
		}
		return util.ReportInfo("FAST mode " + status)
	})
}

// toggleUltracode flips Claude's Ultracode on the shown chat's model.
func (m *UI) toggleUltracode() tea.Cmd {
	if m.isAgentBusy() {
		return util.ReportWarn("Agent is busy, please wait...")
	}
	selected, provider, model, ok := m.shownModelInfo()
	if !ok {
		return util.ReportError(errors.New("no model is selected"))
	}
	if provider == nil || model == nil || !config.SupportsUltracode(*provider, *model) {
		return util.ReportError(errors.New("selected model does not support Ultracode"))
	}
	selected.Ultracode = !selected.Ultracode
	return m.saveShownModel(selected, func(err error) tea.Cmd {
		if err != nil {
			return util.ReportError(err)
		}
		return util.ReportInfo("Ultracode " + onOff(selected.Ultracode))
	})
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}
