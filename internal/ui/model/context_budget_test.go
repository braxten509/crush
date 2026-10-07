package model

import (
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/workspace"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestContextUsesActiveAgentsBoundary(t *testing.T) {
	ws := newLoadWorkspace()
	ws.cfg.Options.AutoCompactTokenLimit = 500000
	ws.cfg.Providers.Set("claude", config.ProviderConfig{ID: "claude", Type: config.TypeClaudeCode})
	u := newLoadTestUI(t, ws)
	model := &workspace.AgentModel{ModelCfg: config.SelectedModel{Provider: "claude", Model: "opus"}, CatwalkCfg: catwalk.Model{ContextWindow: 400000}}
	sess := &session.Session{PromptTokens: 381387, CompletionTokens: 432, ContextBudget: session.ContextBudget{Provider: string(config.TypeClaudeCode), Model: "opus", Tokens: 467000}}
	limit := u.compactionLimit(model, sess)
	require.EqualValues(t, 467000, limit)
	require.Contains(t, ansi.Strip(renderComposerFooter(u.com.Styles, model, sess, limit, nil, 120)), "Context 18% left")
	model.ModelCfg.Model = "sonnet"
	require.Zero(t, u.compactionLimit(model, sess), "never show another model's saved budget")
	model.ModelCfg.Provider = "direct-api"
	require.EqualValues(t, 380000, u.compactionLimit(model, sess), "fallback display includes the same headroom as its trigger")
	model.CatwalkCfg.ContextWindow = 0
	require.EqualValues(t, 500000, u.compactionLimit(model, sess))
}
