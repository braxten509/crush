package model

import (
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/crush/internal/ui/util"
	"github.com/charmbracelet/crush/internal/workspace"
	"github.com/stretchr/testify/require"
)

// refreshConfig delivers a config refresh to u through Update and returns
// the messages its commands produce.
func refreshConfig(u *UI) []tea.Msg {
	_, cmd := u.Update(workspace.ConfigRefreshedMsg{})
	return collectMsgs(cmd)
}

// TestConfigRefreshReappliesTheme: after a config refresh the terminal
// reloads its theme when active_theme names another theme or the theme's
// file changed, and leaves it alone otherwise (G-3).
func TestConfigRefreshReappliesTheme(t *testing.T) {
	u, ws, dir := newThemeUI(t)
	save := func(bg string) {
		require.NoError(t, styles.SaveThemeFile(filepath.Join(dir, "mine.json"),
			&styles.ThemeFile{Base: "graphite", Palette: styles.Palette{BgBase: bg}}))
	}
	save("#101010")
	ws.cfg.Options.TUI.ActiveTheme = "mine"

	msg, ok := findMsg[themeReloadedMsg](refreshConfig(u))
	require.True(t, ok, "a new active_theme reloads the theme")
	u.Update(msg)
	want, err := styles.LoadTheme("mine")
	require.NoError(t, err)
	require.Equal(t, want, *u.com.Styles)

	_, ok = findMsg[themeReloadedMsg](refreshConfig(u))
	require.False(t, ok, "nothing changed: no reload")

	save("#fafafaff") // another size, so the revision changes whatever the clock
	msg, ok = findMsg[themeReloadedMsg](refreshConfig(u))
	require.True(t, ok, "the theme's file changed: reload")
	u.Update(msg)
	want, err = styles.LoadTheme("mine")
	require.NoError(t, err)
	require.Equal(t, want, *u.com.Styles)
}

// TestConfigRefreshReappliesCompactAndScrollbar: compact_mode and scrollbar,
// copied at start, follow a config refresh (G-3).
func TestConfigRefreshReappliesCompactAndScrollbar(t *testing.T) {
	u, ws, _ := newThemeUI(t)
	require.False(t, u.forceCompactMode)
	ws.cfg.Options.TUI.CompactMode = true
	ws.cfg.Options.TUI.Scrollbar = config.ScrollbarNever

	refreshConfig(u)
	require.True(t, u.forceCompactMode)
	require.Equal(t, config.ScrollbarNever, u.chat.scrollbarMode)

	ws.cfg.Options.TUI.CompactMode = false
	refreshConfig(u)
	require.False(t, u.forceCompactMode)
}

// agentNameWorkspace names a main agent and records agent switches.
type agentNameWorkspace struct {
	*cmdStateWorkspace
	agent    string
	switches int
}

func (w *agentNameWorkspace) AgentMainName() (string, error) { return w.agent, nil }

func (w *agentNameWorkspace) AgentSetMain(string) error {
	w.switches++
	return nil
}

// TestConfigRefreshFollowsMainAgent: after a config refresh the mode mark
// follows the engine's main agent, with no banner and no switch of its own
// (G-3, G-26).
func TestConfigRefreshFollowsMainAgent(t *testing.T) {
	u, base, _ := newThemeUI(t)
	ws := &agentNameWorkspace{cmdStateWorkspace: base, agent: config.AgentPlan}
	u.com.Workspace = ws
	require.Equal(t, uiInputModeCode, u.mode)

	msg, ok := findMsg[mainAgentMsg](refreshConfig(u))
	require.True(t, ok)
	_, cmd := u.Update(msg)
	require.Equal(t, uiInputModePlan, u.mode)
	_, banner := findMsg[util.InfoMsg](collectMsgs(cmd))
	require.False(t, banner, "following another app's switch shows no banner")

	ws.agent = config.AgentCoder
	msg, ok = findMsg[mainAgentMsg](refreshConfig(u))
	require.True(t, ok)
	u.Update(msg)
	require.Equal(t, uiInputModeCode, u.mode)
	require.Zero(t, ws.switches, "the terminal does not switch the agent itself")
}
