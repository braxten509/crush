package config

import (
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/stretchr/testify/require"
)

func TestConfig_ValidateFastMode(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Providers: csync.NewMapFrom(map[string]ProviderConfig{
			"claude-code": {ID: "claude-code", Type: TypeClaudeCode, Models: []catwalk.Model{{ID: "opus"}}},
			"codex-cli":   {ID: "codex-cli", Type: TypeCodexCLI, Models: []catwalk.Model{{ID: "gpt-6.1-sol"}}},
			"grok-cli":    {ID: "grok-cli", Type: TypeGrokCLI, Models: []catwalk.Model{{ID: "grok-4.7"}}},
		}),
	}

	t.Run("claude code and codex models have fast mode", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, cfg.ValidateFastMode("claude-code", "opus"))
		require.NoError(t, cfg.ValidateFastMode("codex-cli", "gpt-6.1-sol"))
	})

	t.Run("other providers are rejected", func(t *testing.T) {
		t.Parallel()
		err := cfg.ValidateFastMode("grok-cli", "grok-4.7")
		require.Error(t, err)
		require.Contains(t, err.Error(), "has no fast mode")
	})

	t.Run("unknown model errors", func(t *testing.T) {
		t.Parallel()
		err := cfg.ValidateFastMode("codex-cli", "gpt-9")
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})
}
