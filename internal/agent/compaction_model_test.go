package agent

import (
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/stretchr/testify/require"
)

// A chat compacts on the model set in Settings when there is one it can
// use; otherwise on its own model, as before.
func TestCompactionChoice(t *testing.T) {
	t.Parallel()
	providers := csync.NewMapFrom(map[string]config.ProviderConfig{
		"deepinfra":   {Type: catwalk.TypeOpenAICompat},
		"claude-code": {Type: config.TypeClaudeCode},
		"off":         {Type: catwalk.TypeOpenAI, Disable: true},
	})
	cheap := config.SelectedModel{Provider: "deepinfra", Model: "flash"}
	pricey := config.SelectedModel{Provider: "codex-api", Model: "gpt-6-astra"}
	cfg := func(compact *config.SelectedModel) *config.Config {
		c := &config.Config{Providers: providers, Models: map[config.SelectedModelType]config.SelectedModel{}}
		if compact != nil {
			c.Models[config.SelectedModelTypeCompact] = *compact
		}
		return c
	}

	got, ok := compactionChoice(cfg(&cheap), pricey, false)
	require.True(t, ok)
	require.Equal(t, cheap, got)

	_, ok = compactionChoice(cfg(nil), pricey, false)
	require.False(t, ok, "nothing set: the chat's own model")

	_, ok = compactionChoice(cfg(&cheap), pricey, true)
	require.False(t, ok, "an agent CLI chat keeps its own memory")

	_, ok = compactionChoice(cfg(&config.SelectedModel{Provider: "claude-code", Model: "opus"}), pricey, false)
	require.False(t, ok, "an agent CLI can't compact another chat")

	_, ok = compactionChoice(cfg(&config.SelectedModel{Provider: "off", Model: "x"}), pricey, false)
	require.False(t, ok, "a disabled provider falls back")

	_, ok = compactionChoice(cfg(&cheap), cheap, false)
	require.False(t, ok, "already on that model")
}
