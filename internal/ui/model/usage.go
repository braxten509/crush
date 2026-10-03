package model

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/ui/dialog"
)

type usageTickMsg struct{ dialog *dialog.Usage }
type usageRefreshedMsg struct {
	dialog *dialog.Usage
	err    error
}

func usageTick(d *dialog.Usage) tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return usageTickMsg{dialog: d} })
}

func (m *UI) openUsageDialog() tea.Cmd {
	if d, ok := m.dialog.Dialog(dialog.UsageID).(*dialog.Usage); ok {
		m.dialog.BringToFront(dialog.UsageID)
		return m.refreshUsage(d)
	}
	data := dialog.UsageData{Provider: "Current model", CompactAt: m.compactionLimit()}
	if model := m.selectedLargeModel(); model != nil {
		data.Model, data.ModelID = model.CatwalkCfg.Name, model.ModelCfg.Model
		data.Kind = m.cliKind()
		if cfg := m.com.Config(); cfg != nil && cfg.Providers != nil {
			if provider, ok := cfg.Providers.Get(model.ModelCfg.Provider); ok {
				data.Provider = provider.Name
			}
		}
	}
	d := dialog.NewUsage(m.com, data)
	m.updateUsageData(d)
	m.dialog.OpenDialog(d)
	return tea.Batch(m.refreshUsage(d), usageTick(d))
}

func (m *UI) updateUsageData(d *dialog.Usage) {
	data := d.Data()
	data.Limits = cliagent.Limits(data.Kind, data.ModelID)
	if m.session != nil {
		data.HasContext = true
		data.Context = m.session.PromptTokens + m.session.CompletionTokens
		data.Estimated = m.session.EstimatedUsage
	}
	d.SetData(data)
}

func (m *UI) refreshUsage(d *dialog.Usage) tea.Cmd {
	data := d.Data()
	if data.Kind == "" || data.Refreshing {
		return nil
	}
	data.Refreshing, data.Error = true, ""
	d.SetData(data)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		err := cliagent.RefreshLimitsNow(ctx, data.Kind)
		return usageRefreshedMsg{dialog: d, err: err}
	}
}

func (m *UI) applyUsageRefresh(msg usageRefreshedMsg) {
	if m.dialog.Dialog(dialog.UsageID) != msg.dialog {
		return
	}
	data := msg.dialog.Data()
	data.Refreshing = false
	if msg.err != nil {
		data.Error = msg.err.Error()
	} else {
		data.CheckedAt = time.Now()
	}
	msg.dialog.SetData(data)
	m.updateUsageData(msg.dialog)
}
