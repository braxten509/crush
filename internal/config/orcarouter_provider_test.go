package config

import (
	"context"
	"os"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/env"
	"github.com/stretchr/testify/require"
)

// OrcaRouter is always listed, key or not, and every account of it stays
// with no saved models (its "+" shows) or with the ones picked from its
// catalog, their vendor words dropped.
func TestOrcaRouterProvidersLoad(t *testing.T) {
	listOrcaRouter = true
	t.Cleanup(func() { listOrcaRouter = false })
	cfg := &Config{Providers: csync.NewMapFrom(map[string]ProviderConfig{
		OrcaRouterProviderID + "-2": {
			APIKey: "sk-orca-2",
			Models: []catwalk.Model{{ID: "anthropic/claude-fable-5", Name: "Anthropic: Claude Fable 5"}},
		},
	})}
	cfg.setDefaults("/tmp", "")
	cfg.Options.DisableDefaultProviders = true
	testEnv := env.NewFromMap(map[string]string{"PATH": os.Getenv("PATH")})
	_, err := cfg.configureProviders(context.Background(), testEnv, NewShellVariableResolver(testEnv), nil)
	require.NoError(t, err)

	p, ok := cfg.Providers.Get(OrcaRouterProviderID)
	require.True(t, ok, "listed with no key and no models")
	require.Equal(t, "OrcaRouter", p.Name)
	require.Equal(t, catwalk.TypeOpenAICompat, p.Type)
	require.Equal(t, orcaRouterBaseURL, p.BaseURL)
	require.Empty(t, p.Models)

	two, ok := cfg.Providers.Get(OrcaRouterProviderID + "-2")
	require.True(t, ok)
	require.True(t, IsOrcaRouterProvider(two))
	require.Equal(t, orcaRouterBaseURL, two.BaseURL)
	require.Equal(t, "sk-orca-2", two.APIKey)
	require.Equal(t, "Claude Fable 5", two.Models[0].Name)
	require.False(t, IsOrcaRouterProvider(ProviderConfig{ID: "orcarouterx", BaseURL: "https://example.com/v1"}))
}
