package discover

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/stretchr/testify/require"
)

// echoResolver resolves a value to itself, as a literal API key does.
type echoResolver struct{}

func (echoResolver) ResolveValue(v string) (string, error) { return v, nil }

func TestDiscoverKimiModels(t *testing.T) {
	t.Parallel()

	var gotPath, gotKey, gotVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []any{
				map[string]any{
					"id":           "k3.1-pro",
					"display_name": "Kimi K3.1 Pro",
					"limit":        map[string]any{"context": 262144},
					"reasoning": map[string]any{"type": "only", "effort": map[string]any{
						"valid": []string{"low", "high", "max"}, "default": "high",
					}},
				},
				map[string]any{"id": "k3", "limit": map[string]any{"context": 262144}},
				map[string]any{"id": "kimi-for-coding"},
			},
		})
	}))
	defer srv.Close()

	cfg := Config{
		ID:      "kimi-coding",
		Type:    catwalk.TypeAnthropic,
		BaseURL: srv.URL,
		APIKey:  "sk-test",
		ExistingModels: []catwalk.Model{{
			ID:            "k3",
			Name:          "Kimi K3",
			ContextWindow: 1048576,
			CanReason:     true,
		}},
	}

	models, err := discoverKimiModels(context.Background(), cfg, echoResolver{})
	require.NoError(t, err)
	require.Equal(t, "/v1/models", gotPath)
	require.Equal(t, "sk-test", gotKey)
	require.Equal(t, "2023-06-01", gotVersion)
	require.Len(t, models, 3)

	// The API's order is kept, and its newest model leads.
	require.Equal(t, "k3.1-pro", models[0].ID)
	require.Equal(t, "Kimi K3.1 Pro", models[0].Name)
	require.EqualValues(t, 262144, models[0].ContextWindow)
	require.True(t, models[0].CanReason)
	require.Equal(t, []string{"low", "high", "max"}, models[0].ReasoningLevels)
	require.Equal(t, "high", models[0].DefaultReasoningEffort)
	require.True(t, models[0].SupportsImages)

	// A model the catalog already knows keeps its name, but the API's
	// own context window wins over the catalog's stale one.
	require.Equal(t, "Kimi K3", models[1].Name)
	require.EqualValues(t, 262144, models[1].ContextWindow)

	// A model new to us and without a limit gets the safe defaults.
	require.EqualValues(t, 262144, models[2].ContextWindow)
	require.EqualValues(t, 32768, models[2].DefaultMaxTokens)
}

func TestDiscoverKimiModelsRefused(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := discoverKimiModels(context.Background(), Config{ID: "kimi-coding", BaseURL: srv.URL, APIKey: "sk-test"}, echoResolver{})
	require.Error(t, err)
}
