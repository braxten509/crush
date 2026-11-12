package config

import (
	"context"
	"os"
	"strings"
	"testing"

	"charm.land/catwalk/v3/pkg/catwalk"
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
		require.Equal(t, catwalk.TypeResponses, p.Type)
		require.Equal(t, "https://api.openai.com/v1", p.BaseURL)
		require.Empty(t, p.APIKey)
		require.NotEmpty(t, p.Models)
		ids := map[string]bool{}
		for _, m := range p.Models {
			ids[m.ID] = true
		}
		for id := range ids {
			if base, ok := strings.CutSuffix(id, "-ultrafast"); ok {
				require.True(t, ids[base], "%s is listed without its base model", id)
			}
		}
	})

	t.Run("a saved key and name are kept", func(t *testing.T) {
		t.Parallel()
		c := &Config{Providers: csync.NewMap[string, ProviderConfig]()}
		c.Providers.Set(CodexAPIProviderID, ProviderConfig{APIKey: "sk-test", Name: "Work OpenAI"})
		c.addCodexAPIProvider()
		p, _ := c.Providers.Get(CodexAPIProviderID)
		require.Equal(t, "sk-test", p.APIKey)
		require.Equal(t, "Work OpenAI", p.Name)
		require.NotEmpty(t, p.Models)
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
	require.NotEmpty(t, p.Models)
}

// OpenAI bills a whole request at its long-context rates once its input
// passes 272K tokens: input and cached input 2x, output 1.5x.
func TestCodexAPILongContextPrice(t *testing.T) {
	t.Parallel()
	models := codexAPIModels()
	require.NotEmpty(t, models)
	model := models[0]
	short := PriceForRequest(model, 272000)
	require.Positive(t, short.Pricing.Input)
	require.Equal(t, model.Pricing, short.Pricing, "up to 272K the model's own prices")
	long := PriceForRequest(model, 272001)
	require.InDelta(t, 2*short.Pricing.Input, long.Pricing.Input, 1e-9)
	require.InDelta(t, 2*short.Pricing.CacheHit, long.Pricing.CacheHit, 1e-9)
	require.InDelta(t, 1.5*short.Pricing.Output, long.Pricing.Output, 1e-9)
	other := catwalk.Model{ID: "claude-opus-5-5", Pricing: catwalk.Pricing{Input: 4}}
	require.Equal(t, 4.0, PriceForRequest(other, 900000).Pricing.Input)
}

// The Codex API compacts before a request would reach OpenAI's long-context
// price, unless the switch is off; other providers keep their limit.
func TestCodexAPIPriceStepCompaction(t *testing.T) {
	t.Parallel()
	var none *Options
	require.Equal(t, int64(240000), none.PriceStepCompactionLimit(CodexAPIProviderID, 400000))
	require.Equal(t, int64(100000), none.PriceStepCompactionLimit(CodexAPIProviderID, 100000))
	off := false
	require.Equal(t, int64(400000), (&Options{CodexAPICompactBeforePriceStep: &off}).PriceStepCompactionLimit(CodexAPIProviderID, 400000))
	require.Equal(t, int64(400000), none.PriceStepCompactionLimit("anthropic", 400000))
}
