package model

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/crush/internal/workspace"
)

// Config refresh (desktop design 5-6, section 4 M4; G-3). When the config
// changed somewhere else (the desktop app, another project's engine, a hand
// edit), the client refreshes its config and tells the UI. The UI then
// re-applies the options it copied at start, reloads its theme when
// active_theme names another theme or the theme's file changed, and sets
// its mode mark from the engine's main agent.

// themeReloadedMsg carries a theme loaded after a config refresh.
type themeReloadedMsg struct {
	name, revision string
	styles         styles.Styles
	err            error
}

// mainAgentMsg carries the engine's main agent after a config refresh.
type mainAgentMsg struct {
	agent string
}

// mainAgentNamer is a workspace that can name the engine's main agent.
type mainAgentNamer interface {
	AgentMainName() (string, error)
}

// handleConfigRefreshMsg handles the config refresh and its follow-ups.
// ok is false for any other message.
func (m *UI) handleConfigRefreshMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case workspace.ConfigRefreshedMsg:
		return m.applyConfigRefresh(), true
	case themeReloadedMsg:
		m.applyReloadedTheme(msg)
		return nil, true
	case mainAgentMsg:
		m.followMainAgent(msg.agent)
		return nil, true
	}
	return nil, false
}

// applyConfigRefresh re-applies compact_mode and scrollbar, and starts the
// theme reload and the main agent read when needed.
func (m *UI) applyConfigRefresh() tea.Cmd {
	cfg := m.com.Config()
	if cfg == nil || cfg.Options == nil || cfg.Options.TUI == nil {
		return nil
	}
	tui := cfg.Options.TUI
	var cmds []tea.Cmd
	if m.chat != nil && m.chat.scrollbarMode != tui.Scrollbar {
		m.chat.scrollbarMode = tui.Scrollbar
		m.frame.invalidate()
	}
	if m.forceCompactMode != tui.CompactMode {
		m.forceCompactMode = tui.CompactMode
		if m.forceCompactMode && m.focus == uiFocusSidebar {
			m.sidebar.scrollbarVisible = false
			if m.activeInline != nil {
				m.focusActiveInline(uiFocusEditor)
			} else {
				cmds = append(cmds, m.setFocus(uiFocusEditor, keepChatFocus))
			}
		}
		m.updateLayoutAndSize()
	}

	name := common.ThemeNameFromConfig(cfg)
	revision := styles.ThemeFileRevision(name)
	if m.theme.preview == nil && (!strings.EqualFold(name, m.theme.shown) || revision != m.theme.revision) {
		cmds = append(cmds, func() tea.Msg {
			s, err := styles.LoadTheme(name)
			return themeReloadedMsg{name: name, revision: revision, styles: s, err: err}
		})
	}
	if namer, ok := m.com.Workspace.(mainAgentNamer); ok {
		cmds = append(cmds, func() tea.Msg {
			agent, err := namer.AgentMainName()
			if err != nil {
				return nil
			}
			return mainAgentMsg{agent: agent}
		})
	}
	return tea.Batch(cmds...)
}

// applyReloadedTheme shows a theme reloaded after a config refresh, unless
// a preview started meanwhile. A theme that no longer loads falls back to
// the default, as at start.
func (m *UI) applyReloadedTheme(msg themeReloadedMsg) {
	if m.theme.preview != nil {
		return
	}
	s := msg.styles
	if msg.err != nil {
		s = styles.ThemeFromConfig(msg.name)
	}
	m.theme.shown, m.theme.revision = msg.name, msg.revision
	m.applyTheme(s)
}

// followMainAgent sets the mode mark from the engine's main agent, without
// a banner and without switching the agent: another app switched it.
func (m *UI) followMainAgent(agent string) {
	if m.modeSwitching {
		return
	}
	want := m.mode
	switch agent {
	case config.AgentPlan:
		want = uiInputModePlan
	case config.AgentCoder:
		want = uiInputModeCode
	}
	if want == m.mode {
		return
	}
	m.mode = want
	m.setEditorPrompt(m.yoloModeCached())
}
