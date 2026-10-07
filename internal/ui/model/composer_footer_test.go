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
