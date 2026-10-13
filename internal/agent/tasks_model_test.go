package agent

import (
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/agent/hyper"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

// Background tasks build their models through the same path as the main
// agents, so they get the whole decorator chain and the provider's prompt
// prefix and auto-summarize setting.
func TestTaskSubAgentBuildsModelLikeCoordinator(t *testing.T) {
	env := testEnv(t)
	providerCfg := config.ProviderConfig{
		ID:                 hyper.Name,
		Type:               catwalk.TypeOpenAICompat,
		APIKey:             "test-key",
		SystemPromptPrefix: "prefix for every prompt",
	}
	coord := newTestCoordinator(t, env, hyper.Name, providerCfg)
	coord.permissions = env.permissions
	coord.cfg.Config().Options.DisableAutoSummarize = true

	model := catwalk.Model{ID: "test-model", ContextWindow: 100_000}
	selected := config.SelectedModel{Provider: hyper.Name, Model: model.ID}
	h := &taskHub{c: coord, dir: t.TempDir()}

	sub, m, err := h.subAgent(t.Context(), providerCfg, model, selected, false)
	require.NoError(t, err)

	credits, ok := m.Model.(*hyperCreditsModel)
	require.True(t, ok, "hyper task models refresh the credit balance, got %T", m.Model)
	_, ok = credits.LanguageModel.(requestTimeoutModel)
	require.True(t, ok, "task models are bounded by the request timeout, got %T", credits.LanguageModel)

	agent, ok := sub.(*sessionAgent)
	require.True(t, ok)
	require.Equal(t, providerCfg.SystemPromptPrefix, agent.systemPromptPrefix.Get())
	require.True(t, agent.disableAutoSummarize)
	require.Same(t, m.Model, agent.largeModel.Get().Model)
}
