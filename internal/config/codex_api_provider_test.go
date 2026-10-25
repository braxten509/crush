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

func TestCodexAPIProvider(t *testing.T) {
	t.Parallel()

	t.Run("listed without a key so Settings can add one", func(t *testing.T) {
		t.Parallel()
		c := &Config{Providers: csync.NewMap[string, ProviderConfig]()}
		c.addCodexAPIProvider()
		p, ok := c.Providers.Get(CodexAPIProviderID)
		require.True(t, ok)
		require.Equal(t, "Codex API", p.Name)
		require.Equal(t, catwalk.TypeOpenAI, p.Type)
		require.Equal(t, "https://api.openai.com/v1", p.BaseURL)
		require.Empty(t, p.APIKey)
		var ids []string
		for _, m := range p.Models {
			ids = append(ids, m.ID)
		}
		require.Equal(t, []string{"gpt-6.1-sol", "gpt-6.1-sol-ultrafast", "gpt-6-astra", "gpt-6-astra-ultrafast"}, ids)
	})

	t.Run("a saved key and name are kept", func(t *testing.T) {
		t.Parallel()
		c := &Config{Providers: csync.NewMap[string, ProviderConfig]()}
		c.Providers.Set(CodexAPIProviderID, ProviderConfig{APIKey: "sk-test", Name: "Work OpenAI"})
		c.addCodexAPIProvider()
		p, _ := c.Providers.Get(CodexAPIProviderID)
		require.Equal(t, "sk-test", p.APIKey)
		require.Equal(t, "Work OpenAI", p.Name)
		require.Len(t, p.Models, 4)
	})

	t.Run("a disabled entry stays off", func(t *testing.T) {
		t.Parallel()
		c := &Config{Providers: csync.NewMap[string, ProviderConfig]()}
		c.Providers.Set(CodexAPIProviderID, ProviderConfig{Disable: true})
		c.addCodexAPIProvider()
		p, _ := c.Providers.Get(CodexAPIProviderID)
		require.True(t, p.Disable)
		require.Empty(t, p.Models)
	})

	t.Run("ultrafast ids map to the real model and tier", func(t *testing.T) {
		t.Parallel()
		model, tier := CodexAPIRequestModel("gpt-6.1-sol-ultrafast")
		require.Equal(t, "gpt-6.1-sol", model)
		require.Equal(t, "ultrafast", tier)
		model, tier = CodexAPIRequestModel("gpt-6-astra")
		require.Equal(t, "gpt-6-astra", model)
		require.Empty(t, tier)
	})
}

// A key saved from Settings is the whole config entry; loading must keep it.
func TestCodexAPIKeyOnlyEntrySurvivesLoad(t *testing.T) {
	listCodexAPI = true
	t.Cleanup(func() { listCodexAPI = false })
	cfg := &Config{Providers: csync.NewMapFrom(map[string]ProviderConfig{
		CodexAPIProviderID: {APIKey: "sk-test"},
	})}
	cfg.setDefaults("/tmp", "")
	cfg.Options.DisableDefaultProviders = true
	testEnv := env.NewFromMap(map[string]string{"PATH": os.Getenv("PATH")})
	_, err := cfg.configureProviders(context.Background(), testEnv, NewShellVariableResolver(testEnv), nil)
	require.NoError(t, err)
	p, ok := cfg.Providers.Get(CodexAPIProviderID)
	require.True(t, ok)
	require.Equal(t, "sk-test", p.APIKey)
	require.Len(t, p.Models, 4)
}
