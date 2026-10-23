package config

import (
	"context"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/env"
	"github.com/stretchr/testify/require"
)

// OpenRouter names its models "Vendor: Model" ("DeepSeek: DeepSeek V4.1
// Flash 1017"), which read badly beside the group's own name. Crush drops
// the vendor word, for models already saved as well as the catalog's own
// (owner, 2026-10-22).
func TestOpenRouterModelNamesDropTheVendorWord(t *testing.T) {
	off := false
	cfg := &Config{Providers: csync.NewMap[string, ProviderConfig]()}
	cfg.Providers.Set("openrouter", ProviderConfig{
		Name: "OpenRouter", Type: catwalk.TypeOpenRouter, BaseURL: "https://openrouter.ai/api/v1",
		APIKey: "key", AutoDiscoverModels: &off,
		Models: []catwalk.Model{
			{ID: "deepseek/deepseek-v4.1-flash-1017", Name: "DeepSeek: DeepSeek V4.1 Flash 1017"},
			{ID: "meta-llama/llama-4-maverick", Name: "Meta: Llama 4 Maverick"},
			{ID: "vendor/plain", Name: "Plain Name"},
		},
	})
	cfg.Providers.Set("plain", ProviderConfig{
		Type: catwalk.TypeOpenAICompat, BaseURL: "https://example.invalid/v1",
		APIKey: "key", AutoDiscoverModels: &off,
		Models: []catwalk.Model{{ID: "m", Name: "Maker: Model"}},
	})
	cfg.setDefaults("/tmp", "")
	cfg.Options.DisableDefaultProviders = true
	e := env.NewFromMap(map[string]string{})
	_, err := cfg.configureProviders(context.Background(), e, NewShellVariableResolver(e), nil)
	require.NoError(t, err)

	kept, ok := cfg.Providers.Get("openrouter")
	require.True(t, ok)
	names := map[string]string{}
	for _, m := range kept.Models {
		names[m.ID] = m.Name
	}
	require.Equal(t, "DeepSeek V4.1 Flash 1017", names["deepseek/deepseek-v4.1-flash-1017"])
	require.Equal(t, "Llama 4 Maverick", names["meta-llama/llama-4-maverick"])
	require.Equal(t, "Plain Name", names["vendor/plain"])

	other, ok := cfg.Providers.Get("plain")
	require.True(t, ok)
	require.Equal(t, "Maker: Model", other.Models[0].Name, "another provider keeps its names")
}
