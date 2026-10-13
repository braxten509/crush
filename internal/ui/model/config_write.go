package model

import (
	"errors"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/cpuaffinity"
	"github.com/charmbracelet/crush/internal/ui/util"
)

// configWriteMsg delivers the outcome of an off-thread config write back to
// the Update goroutine, where then runs with the write's error.
type configWriteMsg struct {
	err  error
	then func(err error) tea.Cmd
}

// writeConfigField writes one global config field off the Update goroutine
// (a synchronous HTTP round-trip in client/server mode). then runs on the
// Update goroutine once the write finished, so it may change UI state; the
// command itself captures only locals.
func (m *UI) writeConfigField(key string, value any, then func(err error) tea.Cmd) tea.Cmd {
	ws := m.com.Workspace
	return func() tea.Msg {
		return configWriteMsg{
			err:  ws.SetConfigField(config.ScopeGlobal, key, value),
			then: then,
		}
	}
}

// applyConfigWrite finishes an off-thread config write on the Update
// goroutine.
func (m *UI) applyConfigWrite(msg configWriteMsg) tea.Cmd {
	if msg.then == nil {
		return nil
	}
	return msg.then(msg.err)
}

// toggleTransparentBackground flips options.tui.transparent. The config
// write runs off the Update goroutine; the UI switches backgrounds only once
// it succeeded.
func (m *UI) toggleTransparentBackground() tea.Cmd {
	cfg := m.com.Config()
	if cfg == nil {
		return util.ReportError(errors.New("configuration not found"))
	}
	newValue := !(cfg.Options != nil && cfg.Options.TUI.IsTransparent())
	return m.writeConfigField("options.tui.transparent", newValue, func(err error) tea.Cmd {
		if err != nil {
			return util.ReportError(err)
		}
		m.isTransparent = newValue
		if newValue {
			return util.ReportInfo("Using the terminal's background")
		}
		return util.ReportInfo("Using Crush's background")
	})
}

// selectNotificationStyle switches the notification style. The in-memory
// config and the notification backend change at once; the config write runs
// off the Update goroutine and reports how it went.
func (m *UI) selectNotificationStyle(style string) tea.Cmd {
	cfg := m.com.Config()
	if cfg == nil || cfg.Options == nil {
		return nil
	}
	cfg.Options.Notifications = style
	// Reinitialize notification backend with new style.
	m.notifyBackend = selectNotificationBackend(m.caps, cfg)
	return m.writeConfigField("options.notifications", style, func(err error) tea.Cmd {
		if err != nil {
			return util.ReportError(err)
		}
		return util.ReportInfo("Notifications set to: " + style)
	})
}

// toggleAgentCPUs flips options.agent_cpus_enabled and moves Crush and its
// processes onto the agent CPUs, or back onto all of them. The config write
// and the move both run off the Update goroutine.
func (m *UI) toggleAgentCPUs() tea.Cmd {
	cfg := m.com.Config()
	newValue := cfg == nil || cfg.Options == nil || !cfg.Options.AgentCPUsEnabled
	cpus := ""
	if cfg != nil && cfg.Options != nil {
		cpus = cfg.Options.AgentCPUs
	}
	ws := m.com.Workspace
	return func() tea.Msg {
		if err := ws.SetConfigField(config.ScopeGlobal, "options.agent_cpus_enabled", newValue); err != nil {
			return util.ReportError(err)()
		}
		if !newValue {
			if err := cpuaffinity.Reset(); err != nil {
				return util.ReportError(err)()
			}
			return util.NewInfoMsg("AI work can use the whole CPU again")
		}
		list, err := cpuaffinity.Apply(cpus)
		if err != nil {
			return util.ReportError(err)()
		}
		return util.NewInfoMsg("AI work now runs on CPUs " + list)
	}
}

// toggleComposerFocusOnly flips options.tui.composer_focus_only. Once the
// config write succeeded, turning it on hands focus back to the composer.
func (m *UI) toggleComposerFocusOnly() tea.Cmd {
	newValue := !m.composerFocusOnly()
	return m.writeConfigField("options.tui.composer_focus_only", newValue, func(err error) tea.Cmd {
		if err != nil {
			return util.ReportError(err)
		}
		var cmds []tea.Cmd
		if newValue && m.focus != uiFocusEditor {
			// Hand focus back to the composer it now keeps.
			if m.activeInline != nil {
				m.focusActiveInline(uiFocusEditor)
			} else {
				cmds = append(cmds, m.setFocus(uiFocusEditor))
			}
		}
		status := "Tab moves focus again"
		if newValue {
			status = "Focus stays on the composer"
		}
		return tea.Batch(append(cmds, util.ReportInfo(status))...)
	})
}
