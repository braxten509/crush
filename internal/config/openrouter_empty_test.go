package config

import (
	"context"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/env"
	"github.com/stretchr/testify/require"
)

// OpenRouter's saved models are picked from its catalog in the app, so
// the provider stays with none saved yet (its "+" shows); any other
// custom provider without models is still dropped.
func TestOpenRouterStaysWithNoSavedModels(t *testing.T) {
	off := false
	cfg := &Config{Providers: csync.NewMap[string, ProviderConfig]()}
	cfg.Providers.Set("openrouter", ProviderConfig{
		Name: "OpenRouter", Type: catwalk.TypeOpenRouter, BaseURL: "https://openrouter.ai/api/v1",
		APIKey: "key", AutoDiscoverModels: &off,
	})
	cfg.Providers.Set("empty", ProviderConfig{
		Type: catwalk.TypeOpenAICompat, BaseURL: "https://example.invalid/v1",
		APIKey: "key", AutoDiscoverModels: &off,
	})
	cfg.setDefaults("/tmp", "")
	cfg.Options.DisableDefaultProviders = true
	e := env.NewFromMap(map[string]string{})
	_, err := cfg.configureProviders(context.Background(), e, NewShellVariableResolver(e), nil)
	require.NoError(t, err)

	kept, ok := cfg.Providers.Get("openrouter")
	require.True(t, ok, "OpenRouter with no saved models stays")
	require.Empty(t, kept.Models)
	_, ok = cfg.Providers.Get("empty")
	require.False(t, ok, "another provider with no models is dropped")
}
