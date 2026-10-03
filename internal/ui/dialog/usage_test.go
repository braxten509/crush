package dialog

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestUsageShowsExactResetAndDetails(t *testing.T) {
	t.Parallel()
	sty := styles.ThemeFromConfig("claude-code")
	reset := time.Date(2026, 10, 3, 12, 23, 45, 0, time.UTC)
	d := NewUsage(&common.Common{Styles: &sty}, UsageData{
		Kind: config.TypeClaudeCode, Provider: "Claude Code", Model: "Claude Opus",
		Limits:     []cliagent.Limit{{Name: "5h", Used: 100, ResetsAt: reset}, {Name: "Weekly", Used: 39}},
		HasContext: true, Context: 15870, CompactAt: 400000,
	})
	d.now = func() time.Time { return reset.Add(-time.Hour - time.Minute*2 - time.Second*3) }
	body := ansi.Strip(d.body())
	require.Contains(t, body, "100.0% used · 0.0% left")
	require.Contains(t, body, reset.Local().Format("Mon, Jan 2, 2006 · 3:04:05 PM MST (UTC-07:00)"))
	require.Contains(t, body, "In: 01h 02m 03s")
	require.Contains(t, body, "39.0% used · 61.0% left")
	require.Contains(t, body, "Reset time not reported")
	require.Contains(t, body, "15,870 / 400,000 tokens before compaction")
	for _, size := range []struct{ w, h int }{{100, 35}, {60, 24}, {32, 12}} {
		scr := uv.NewScreenBuffer(size.w, size.h)
		d.Draw(scr, scr.Bounds())
		out := ansi.Strip(scr.Render())
		require.Contains(t, out, "Usage")
		require.Contains(t, out, "esc")
		t.Logf("%dx%d:\n%s", size.w, size.h, out)
		for range 30 {
			d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyDown})
		}
		scr = uv.NewScreenBuffer(size.w, size.h)
		d.Draw(scr, scr.Bounds())
		require.Contains(t, ansi.Strip(scr.Render()), "remaining")
		d.offset = 0
	}
	require.Equal(t, ActionRefreshUsage{}, d.HandleMsg(tea.KeyPressMsg{Code: 'r'}))
	d.data.Refreshing = true
	require.Nil(t, d.HandleMsg(tea.KeyPressMsg{Code: 'r'}))
	require.Equal(t, ActionClose{}, d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEscape}))
	d.now = func() time.Time { return reset.Add(time.Second) }
	require.Contains(t, ansi.Strip(d.body()), "Reset time passed")
	require.Contains(t, ansi.Strip(d.body()), "100.0% used", "an old timestamp must not invent new quota")
}

func TestUsageCommandRegistered(t *testing.T) {
	t.Parallel()
	sty := styles.CharmtonePantera()
	c := &Commands{com: &common.Common{Styles: &sty, Workspace: fastModeWorkspace{cfg: ultracodeConfig(config.TypeClaudeCode, nil, false)}}}
	for _, item := range c.defaultCommands() {
		if item.ID() == "usage" {
			require.Equal(t, ActionOpenDialog{UsageID}, item.Action())
			return
		}
	}
	t.Fatal("/usage command missing")
}
