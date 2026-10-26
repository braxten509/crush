package config

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/env"
	"github.com/stretchr/testify/require"
)

// Tinfoil's own model list decides what Crush offers: only chat models that
// can use tools, with their prices, sizes and thinking settings.
func TestTinfoilModelList(t *testing.T) {
	body, err := os.ReadFile("testdata/tinfoil-models.json")
	require.NoError(t, err)
	models, err := LoadTinfoilModels(body)
	require.NoError(t, err)
	byID := map[string]catwalk.Model{}
	for _, m := range models {
		byID[m.ID] = m
	}
	require.ElementsMatch(t, []string{"deepseek-v4-1-flash", "glm-5-3", "kimi-k3", "llama3-3-70b", "gemma4-31b", "gpt-oss-120b"}, keysOf(byID))

	ds := byID["deepseek-v4-1-flash"]
	require.Equal(t, "DeepSeek V4.1 Flash", ds.Name)
	require.Equal(t, int64(1048576), ds.ContextWindow)
	require.Equal(t, 0.65, ds.CostPer1MIn)
	require.Equal(t, 1.45, ds.CostPer1MOut)
	require.Equal(t, 0.13, ds.CostPer1MOutCached)
	require.True(t, ds.CanReason)
	require.True(t, ds.SupportsImages)
	require.Equal(t, []string{"low", "medium", "high"}, ds.ReasoningLevels)
	require.Positive(t, ds.DefaultMaxTokens)

	require.True(t, byID["gemma4-31b"].CanReason)
	require.Empty(t, byID["gemma4-31b"].ReasoningLevels)
	require.False(t, byID["llama3-3-70b"].CanReason)

	r, ok := TinfoilThinking("deepseek-v4-1-flash")
	require.True(t, ok)
	require.Equal(t, "xhigh", r.EffortMap["high"])
	require.Equal(t, map[string]any{"chat_template_kwargs": map[string]any{"thinking": false}}, r.Disable)
}

// Every Tinfoil account gets the live list; with Tinfoil out of reach the
// last list read is used.
func TestTinfoilProvidersLoad(t *testing.T) {
	body, err := os.ReadFile("testdata/tinfoil-models.json")
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Setenv("CRUSH_CACHE_DIR", t.TempDir())
	listTinfoil, tinfoilModelsURL = true, server.URL+"/v1/models"
	t.Cleanup(func() { listTinfoil, tinfoilModelsURL = false, tinfoilDefaultModelsURL })

	load := func() *Config {
		cfg := &Config{Providers: csync.NewMapFrom(map[string]ProviderConfig{
			TinfoilProviderID: {APIKey: "key-1"},
			// An account added in Settings: a copy saved with an old list.
			TinfoilProviderID + "-2": {
				Name: "Tinfoil (2)", Type: catwalk.TypeOpenAICompat, BaseURL: tinfoilBaseURL, APIKey: "key-2",
				Models: []catwalk.Model{{ID: "old-model"}},
			},
		})}
		cfg.setDefaults("/tmp", "")
		cfg.Options.DisableDefaultProviders = true
		testEnv := env.NewFromMap(map[string]string{"PATH": os.Getenv("PATH")})
		_, err := cfg.configureProviders(context.Background(), testEnv, NewShellVariableResolver(testEnv), nil)
		require.NoError(t, err)
		return cfg
	}
	check := func(cfg *Config) {
		for _, id := range []string{TinfoilProviderID, TinfoilProviderID + "-2"} {
			p, ok := cfg.Providers.Get(id)
			require.True(t, ok, id)
			require.True(t, IsTinfoilProvider(p), id)
			require.Len(t, p.Models, 6, id)
			require.Equal(t, tinfoilBaseURL, p.BaseURL)
		}
		p, _ := cfg.Providers.Get(TinfoilProviderID)
		require.Equal(t, "Tinfoil", p.Name)
		require.Equal(t, "key-1", p.APIKey)
	}
	check(load())
	server.Close()
	check(load())
}

func keysOf[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
