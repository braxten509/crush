package model

import (
	"strings"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/crush/internal/workspace"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestComposerFooterMatchesApprovedCopy(t *testing.T) {
	t.Parallel()
	sty := styles.ThemeFromConfig("claude-code")
	model := &workspace.AgentModel{
		CatwalkCfg: catwalk.Model{Name: "GPT-6-Astra"},
		ModelCfg:   config.SelectedModel{ReasoningEffort: "high", ServiceTier: "fast"},
	}
	sess := &session.Session{PromptTokens: 67000, CompletionTokens: 1000}
	limits := []cliagent.Limit{{Name: "Weekly", Used: 9}}
	view := renderComposerFooter(&sty, model, sess, 400000, limits, 120)
	require.Equal(t, "GPT-6-Astra high fast · 68k/400k tokens · Context 83% left · Weekly 91% left", ansi.Strip(view))
	for _, width := range []int{120, 80, 40} {
		view = renderComposerFooter(&sty, model, sess, 400000, limits, width)
		for _, line := range strings.Split(view, "\n") {
			require.LessOrEqual(t, ansi.StringWidth(line), width)
		}
		scr := uv.NewScreenBuffer(width, 5)
		uv.NewStyledString(view).Draw(scr, scr.Bounds())
		t.Logf("%d columns:\n%s", width, scr.Render())
		require.Contains(t, ansi.Strip(scr.Render()), "Weekly 91% left")
	}
	sess.PromptTokens = 450000
	require.Contains(t, ansi.Strip(renderComposerFooter(&sty, model, sess, 400000, nil, 120)), "Context 0% left")
	require.Empty(t, renderComposerFooter(&sty, model, sess, 400000, nil, 0))
}

func TestSubagentFooterUsesChildModelSettings(t *testing.T) {
	t.Parallel()
	ws := newLoadWorkspace()
	ws.cfg.Providers.Set("codex-cli", config.ProviderConfig{
		ID: "codex-cli", Type: config.TypeCodexCLI,
		Models: []catwalk.Model{{ID: "child-model", Name: "GPT-6-Astra"}},
	})
	u := newLoadTestUI(t, ws)
	u.agentModel = workspace.AgentModel{CatwalkCfg: catwalk.Model{Name: "Parent model"}}
	u.subagentView = &subagentView{
		task: agent.Task{CLI: "codex", Model: "child-model", Effort: "high", Fast: true},
		session: &session.Session{PromptTokens: 68000, ContextBudget: session.ContextBudget{
			Provider: string(config.TypeCodexCLI), Model: "child-model", Tokens: 400000,
		}},
	}
	view := ansi.Strip(u.subagentFooter(120))
	require.Equal(t, "GPT-6-Astra high fast · 68k/400k tokens · Context 83% left", view)
	require.Equal(t, "Parent model", u.agentModel.CatwalkCfg.Name)
	ws.cfg.Providers.Del("codex-cli")
	require.Contains(t, ansi.Strip(u.subagentFooter(120)), "child-model high fast")
}

// The terminal shows the selected model's account: its own usage and its
// free refills, not the tool's own login's (fallback order, 2026-10-21).
func TestTerminalShowsTheSelectedAccount(t *testing.T) {
	ws := newLoadWorkspace()
	ws.cfg.Providers.Set("codex-cli", config.ProviderConfig{
		ID: "codex-cli", Type: config.TypeCodexCLI,
		Models: []catwalk.Model{{ID: "gpt-test", Name: "GPT test"}},
	})
	u := newLoadTestUI(t, ws)
	u.agentReady = true
	u.agentModel = workspace.AgentModel{ModelCfg: config.SelectedModel{Provider: "codex-cli", Model: "gpt-test", Account: "codex-footer-test"}}
	cliagent.StoreAccountLimits(config.TypeCodexCLI, "codex-footer-test", []cliagent.Limit{{Name: "5h", Used: 30}}, 2)

	limits := u.selectedCLILimits()
	require.Len(t, limits, 1)
	require.InDelta(t, 70, limits[0].Left(), 0.01)
	require.Equal(t, 2, u.selectedCLIRefills())
	sty := styles.ThemeFromConfig("claude-code")
	require.Contains(t, ansi.Strip(usageLimits(&sty, limits, u.selectedCLIRefills(), 60)), "Free refills 2")
	require.NotContains(t, ansi.Strip(usageLimits(&sty, limits, 0, 60)), "refills")
	require.Equal(t, "1 free refill", refillsText(1))
}
