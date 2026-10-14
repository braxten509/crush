package model

import (
	"fmt"
	"log/slog"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/memlimit"
	"github.com/charmbracelet/crush/internal/memwatch"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/util"
)

func (m *UI) openMemoryLimitDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.MemoryLimitID) {
		m.dialog.BringToFront(dialog.MemoryLimitID)
		return nil
	}
	d, err := dialog.NewMemoryLimit(m.com)
	if err != nil {
		return util.ReportError(err)
	}
	m.dialog.OpenDialog(d)
	return nil
}

// setMemoryLimit saves the limit and applies it to the box every Crush
// window shares, so it takes effect everywhere at once.
func (m *UI) setMemoryLimit(gb float64) tea.Cmd {
	if err := config.ValidateMemoryLimitGB(gb); err != nil {
		return util.ReportError(err)
	}
	m.dialog.CloseDialog(dialog.MemoryLimitID)
	ws := m.com.Workspace
	return func() tea.Msg {
		if err := ws.SetConfigField(config.ScopeGlobal, "options.memory_limit_gb", gb); err != nil {
			return util.ReportError(err)()
		}
		if err := memlimit.Apply(memlimit.Slice, config.GBToBytes(gb)); err != nil {
			return util.ReportError(fmt.Errorf("saved, but couldn't apply the memory limit: %w", err))()
		}
		if gb == 0 {
			return util.NewInfoMsg("Memory limit removed")
		}
		return util.NewInfoMsg(fmt.Sprintf("Memory limit set to %g GB for every Crush window", gb))
	}
}

// applySavedMemoryLimit puts the saved limit on the shared box when Crush
// starts. Every window does this, and all read the same saved limit.
func (m *UI) applySavedMemoryLimit() tea.Cmd {
	cfg := m.com.Config()
	if cfg == nil {
		return nil
	}
	limit := cfg.Options.MemoryLimitBytes(memwatch.TotalRAM())
	return func() tea.Msg {
		if _, inBox := memlimit.Usage(memlimit.Slice); !inBox {
			return nil
		}
		if err := memlimit.Apply(memlimit.Slice, limit); err != nil {
			slog.Warn("Could not apply the memory limit", "error", err)
		}
		return nil
	}
}
