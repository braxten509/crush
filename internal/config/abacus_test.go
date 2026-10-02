package config

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/env"
	"github.com/stretchr/testify/require"
)

func TestAbacusControlsSurviveModelResolution(t *testing.T) {
	model := catwalk.Model{ID: "gpt-6.1-sol", DefaultMaxTokens: 128000, CanReason: true, ReasoningLevels: []string{"low", "medium", "high", "xhigh", "max"}, DefaultReasoningEffort: "medium"}
	cfg := &Config{
		Options:   &Options{DisableDefaultProviders: true},
		Providers: csync.NewMapFrom(map[string]ProviderConfig{"abacus": {ID: "abacus", APIKey: "test-key", Models: []catwalk.Model{model}}}),
		Models: map[SelectedModelType]SelectedModel{
			SelectedModelTypeLarge: {Provider: "abacus", Model: model.ID, ServiceTier: "fast", ReasoningEffort: "max", MaxTokens: 64000},
			SelectedModelTypeSmall: {Provider: "abacus", Model: model.ID, ServiceTier: "default", ReasoningEffort: "low", MaxTokens: 8192},
		},
	}
	resolved, err := resolveSelectedModels(cfg, nil)
	require.NoError(t, err)
	require.Equal(t, "fast", resolved.Large.ServiceTier)
	require.Equal(t, "default", resolved.Small.ServiceTier)
	require.Equal(t, "max", resolved.Large.ReasoningEffort)
	require.Equal(t, int64(64000), resolved.Large.MaxTokens)
}

func TestAbacusDiscoveryUsesFreshControlsWithDefaultProvidersDisabled(t *testing.T) {
	resetProviderState()
	t.Cleanup(resetProviderState)
	t.Setenv("CRUSH_GLOBAL_DATA", t.TempDir())
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/providers":
			require.NoError(t, json.NewEncoder(w).Encode([]catwalk.Provider{{ID: "anthropic", Models: []catwalk.Model{{
				ID: "claude-new-version", CanReason: true, ReasoningLevels: []string{"low", "high"}, DefaultReasoningEffort: "high",
			}}}}))
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"claude-new-version","display_name":"Claude New Version","model_type":"text_generation","api_formats":["anthropic"],"tools":true}]}`))
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	t.Setenv("CATWALK_URL", server.URL)
	cfg := &Config{
		Options: &Options{DisableDefaultProviders: true},
		Providers: csync.NewMapFrom(map[string]ProviderConfig{
			AbacusProviderID: {ID: AbacusProviderID, APIKey: "test-key", BaseURL: server.URL + "/v1"},
		}),
	}
	cfg.setDefaults(t.TempDir(), "")
	providers, err := Providers(cfg)
	require.NoError(t, err)
	require.Empty(t, providers, "metadata-only providers must stay out of model selection")
	require.Len(t, ModelCatalog(), 1)
	variables := env.NewFromMap(map[string]string{})
	require.NoError(t, cfg.configureProviders(t.Context(), testStore(cfg), variables, NewShellVariableResolver(variables), providers))
	require.Equal(t, []string{"/v2/providers", "/v1/models"}, requests)
	_, enabled := cfg.Providers.Get("anthropic")
	require.False(t, enabled, "native catalogue metadata must not enable default providers")
	abacus, ok := cfg.Providers.Get(AbacusProviderID)
	require.True(t, ok)
	require.Len(t, abacus.Models, 1)
	require.Equal(t, []string{"low", "high"}, abacus.Models[0].ReasoningLevels)
	require.Equal(t, "high", abacus.Models[0].DefaultReasoningEffort)
}
