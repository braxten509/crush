package app

import (
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/stretchr/testify/require"
)

// An old chat without a model adopts the model it last replied with, else
// the old shared model, else the last pick (owner, 2026-10-24).
func TestOldChatsAdoptTheirLastReplyModel(t *testing.T) {
	cfg := &config.Config{
		Providers: csync.NewMapFrom(map[string]config.ProviderConfig{
			"codex-cli":   {ID: "codex-cli", Type: config.TypeCodexCLI, Models: []catwalk.Model{{ID: "gpt-6-sol", DefaultMaxTokens: 1000, ReasoningLevels: []string{"high", "max"}}}},
			"claude-code": {ID: "claude-code", Type: config.TypeClaudeCode, Models: []catwalk.Model{{ID: "opus"}}},
		}),
		ModelEfforts: map[string]string{"codex-cli:gpt-6-sol": "max"},
	}
	pick := config.SelectedModel{Provider: "claude-code", Model: "opus", ReasoningEffort: "high"}
	shared := config.SelectedModel{Provider: "codex-cli", Model: "gpt-6-luna", Account: "codex-2"}
	lastPick := func() (config.SelectedModel, bool) { return pick, true }

	// The last reply's model, with its remembered effort, on the old shared
	// model's account when it is the same tool.
	got, ok := adoptedModel(cfg, "codex-cli", "gpt-6-sol", shared, true, lastPick)
	require.True(t, ok)
	require.Equal(t, config.SelectedModel{Provider: "codex-cli", Model: "gpt-6-sol", ReasoningEffort: "max", MaxTokens: 1000, Account: "codex-2"}, got)

	// A reply model that is gone: the old shared model.
	got, ok = adoptedModel(cfg, "codex-cli", "gpt-old", shared, true, lastPick)
	require.True(t, ok)
	require.Equal(t, shared, got)

	// No old shared model either: the last pick.
	got, ok = adoptedModel(cfg, "", "", config.SelectedModel{}, false, lastPick)
	require.True(t, ok)
	require.Equal(t, pick, got)
}
