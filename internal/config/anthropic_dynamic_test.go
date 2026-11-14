package config

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"charm.land/catwalk/v3/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/env"
	"github.com/stretchr/testify/require"
)

func TestConfigureProviders_AnthropicDynamicModels(t *testing.T) {
	prev := probeDynamicProviders
	probeDynamicProviders = true
	t.Cleanup(func() { probeDynamicProviders = prev })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/models", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []any{
				map[string]any{
					"id":               "claude-live-1",
					"display_name":     "Claude Live 1",
					"max_input_tokens": 1000000,
					"max_tokens":       128000,
				},
			},
			"has_more": false,
		})
	}))
	defer srv.Close()

	known := []catwalk.Provider{{
		ID:          catwalk.InferenceProviderAnthropic,
		Name:        "Anthropic",
		APIKey:      "$ANTHROPIC_API_KEY",
		APIEndpoint: srv.URL,
		Type:        catwalk.TypeMessages,
		Models: []catwalk.Model{
			{ID: "claude-fixed-only", Name: "Fixed"},
		},
	}}

	cfg := &Config{}
	cfg.setDefaults(t.TempDir(), "")
	testEnv := env.NewFromMap(map[string]string{"ANTHROPIC_API_KEY": "sk-ant-test"})
	_, err := cfg.configureProviders(context.Background(), testEnv, NewShellVariableResolver(testEnv), known)
	require.NoError(t, err)

	anthropic, ok := cfg.Providers.Get(string(catwalk.InferenceProviderAnthropic))
	require.True(t, ok)
	require.True(t, anthropic.DynamicModels)
	require.Len(t, anthropic.Models, 1)
	require.Equal(t, "claude-live-1", anthropic.Models[0].ID)
}

func TestConfigureProviders_AnthropicDynamicFallbackOnError(t *testing.T) {
	prev := probeDynamicProviders
	probeDynamicProviders = true
	t.Cleanup(func() { probeDynamicProviders = prev })

	known := []catwalk.Provider{{
		ID:          catwalk.InferenceProviderAnthropic,
		Name:        "Anthropic",
		APIKey:      "$ANTHROPIC_API_KEY",
		APIEndpoint: closedServerURL(t),
		Type:        catwalk.TypeMessages,
		Models: []catwalk.Model{
			{ID: "claude-fallback", Name: "Fallback"},
		},
	}}

	cfg := &Config{}
	cfg.setDefaults(t.TempDir(), "")
	testEnv := env.NewFromMap(map[string]string{"ANTHROPIC_API_KEY": "sk-ant-test"})
	_, err := cfg.configureProviders(context.Background(), testEnv, NewShellVariableResolver(testEnv), known)
	require.NoError(t, err)

	anthropic, ok := cfg.Providers.Get(string(catwalk.InferenceProviderAnthropic))
	require.True(t, ok)
	require.False(t, anthropic.DynamicModels)
	require.Len(t, anthropic.Models, 1)
	require.Equal(t, "claude-fallback", anthropic.Models[0].ID)
}
