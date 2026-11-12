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
	for _, want := range []string{"100.0%", "0.0%", "39.0%", "61.0%", "01h 02m 03s", "15,870", "400,000"} {
		require.Contains(t, body, want)
	}
	// Every size draws and scrolls without a panic.
	for _, size := range []struct{ w, h int }{{100, 35}, {60, 24}, {32, 12}} {
		scr := uv.NewScreenBuffer(size.w, size.h)
		d.Draw(scr, scr.Bounds())
		for range 30 {
			d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyDown})
		}
		scr = uv.NewScreenBuffer(size.w, size.h)
		d.Draw(scr, scr.Bounds())
		d.offset = 0
	}
	require.Equal(t, ActionRefreshUsage{}, d.HandleMsg(tea.KeyPressMsg{Code: 'r'}))
	d.data.Refreshing = true
	require.Nil(t, d.HandleMsg(tea.KeyPressMsg{Code: 'r'}))
	require.Equal(t, ActionClose{}, d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEscape}))
	d.now = func() time.Time { return reset.Add(time.Second) }
	require.Contains(t, ansi.Strip(d.body()), "100.0%", "an old timestamp must not invent new quota")
}

func TestUsageCommandRegistered(t *testing.T) {
	t.Parallel()
	sty := styles.CharmtonePantera()
	c := &Commands{com: ultracodeCommon(&sty, config.TypeClaudeCode, nil, false)}
	for _, item := range c.defaultCommands() {
		if item.ID() == "usage" {
			require.Equal(t, ActionOpenDialog{UsageID}, item.Action())
			return
		}
	}
	t.Fatal("/usage command missing")
}
