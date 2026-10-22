package config

import (
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/stretchr/testify/require"
)

func effortsConfig() *Config {
	cfg := &Config{Providers: csync.NewMap[string, ProviderConfig]()}
	levels := []string{"low", "medium", "high", "xhigh", "max"}
	cfg.Providers.Set("claude-code", ProviderConfig{ID: "claude-code", Models: []catwalk.Model{
		{ID: "opus", CanReason: true, ReasoningLevels: levels, DefaultReasoningEffort: "high", DefaultMaxTokens: 32_000},
		{ID: "sonnet", CanReason: true, ReasoningLevels: levels, DefaultReasoningEffort: "high", DefaultMaxTokens: 32_000},
	}})
	cfg.Providers.Set("openrouter", ProviderConfig{ID: "openrouter", Models: []catwalk.Model{
		{ID: "moonshot/kimi-k3", CanReason: true, ReasoningLevels: []string{"low", "medium", "high"}, DefaultReasoningEffort: "medium"},
	}})
	return cfg
}

// Each model keeps the effort last used with it (owner, 2026-10-22: "keep
// the selected effort level I had before"), saved with the model choice.
func TestEachModelRemembersItsEffort(t *testing.T) {
	dir := t.TempDir()
	store := testStoreWithPath(effortsConfig(), dir)

	require.NoError(t, store.UpdatePreferredModel(ScopeGlobal, SelectedModelTypeLarge, SelectedModel{Provider: "claude-code", Model: "opus", ReasoningEffort: "max"}))
	require.NoError(t, store.UpdatePreferredModel(ScopeGlobal, SelectedModelTypeLarge, SelectedModel{Provider: "claude-code", Model: "sonnet", ReasoningEffort: "low"}))

	cfg := store.Config()
	require.Equal(t, "max", cfg.ModelEffort("claude-code", "opus"))
	require.Equal(t, "low", cfg.ModelEffort("claude-code", "sonnet"))
	// Never chosen: the model's own default.
	require.Equal(t, "medium", cfg.ModelEffort("openrouter", "moonshot/kimi-k3"))
	// Unknown model: nothing.
	require.Empty(t, cfg.ModelEffort("claude-code", "nope"))

	// Saved in the file, so the next start and the other apps know it.
	saved := readConfigJSON(t, store.globalDataPath)["model_efforts"].(map[string]any)
	require.Equal(t, "max", saved["claude-code:opus"])
	require.Equal(t, "low", saved["claude-code:sonnet"])

	// A remembered level the model no longer has falls back to its default.
	cfg.ModelEfforts["openrouter:moonshot/kimi-k3"] = "max"
	require.Equal(t, "medium", cfg.ModelEffort("openrouter", "moonshot/kimi-k3"))

	// The choice a picker or a chat's restore makes carries it.
	require.Equal(t, SelectedModel{Provider: "claude-code", Model: "opus", ReasoningEffort: "max", MaxTokens: 32_000}, cfg.Remembered("claude-code", "opus"))
}
