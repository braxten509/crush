package discover

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"charm.land/catwalk/v3/pkg/catwalk"
	"github.com/stretchr/testify/require"
)

func TestDiscoverAnthropicModels(t *testing.T) {
	t.Parallel()

	var pages int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/models", r.URL.Path)
		require.Equal(t, "sk-ant-test", r.Header.Get("x-api-key"))
		require.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
		require.Empty(t, r.Header.Get("Authorization"))

		pages++
		after := r.URL.Query().Get("after_id")
		w.Header().Set("Content-Type", "application/json")
		if after == "" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []any{
					map[string]any{
						"id":               "claude-opus-5-5",
						"display_name":     "Claude Opus 5.5",
						"max_input_tokens": 1000000,
						"max_tokens":       128000,
						"capabilities": map[string]any{
							"image_input": map[string]any{"supported": true},
							"thinking": map[string]any{
								"supported": true,
								"types":     map[string]any{"adaptive": map[string]any{"supported": true}},
							},
							"effort": map[string]any{
								"supported": true,
								"low":       map[string]any{"supported": true},
								"medium":    map[string]any{"supported": true},
								"high":      map[string]any{"supported": true},
								"max":       map[string]any{"supported": true},
							},
						},
					},
				},
				"has_more": true,
				"last_id":  "claude-opus-5-5",
			})
			return
		}
		require.Equal(t, "claude-opus-5-5", after)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []any{
				map[string]any{
					"id":               "claude-next-6",
					"display_name":     "Claude Next 6",
					"max_input_tokens": 500000,
					"max_tokens":       64000,
					"capabilities": map[string]any{
						"image_input": map[string]any{"supported": false},
						"thinking": map[string]any{
							"supported": true,
							"types":     map[string]any{"disabled": map[string]any{"supported": false}},
						},
						"effort": map[string]any{"supported": false},
					},
				},
			},
			"has_more": false,
			"last_id":  "claude-next-6",
		})
	}))
	defer srv.Close()

	cfg := Config{
		ID:      "anthropic",
		Type:    catwalk.TypeMessages,
		BaseURL: srv.URL + "/v1",
		APIKey:  "sk-ant-test",
		ExistingModels: []catwalk.Model{{
			ID:             "claude-opus-5-5",
			Pricing:        catwalk.Pricing{Input: 4, Output: 20},
			MaxAttachments: 600,
			Reasoning: catwalk.Reasoning{
				DefaultEffortLevel: "medium",
			},
		}},
	}

	models, err := DiscoverModels(context.Background(), cfg, echoResolver{})
	require.NoError(t, err)
	require.Equal(t, 2, pages)
	require.Len(t, models, 2)

	require.Equal(t, "claude-opus-5-5", models[0].ID)
	require.Equal(t, "Claude Opus 5.5", models[0].Name)
	require.EqualValues(t, 1000000, models[0].ContextWindow)
	require.EqualValues(t, 128000, models[0].DefaultMaxTokens)
	require.EqualValues(t, 4, models[0].Pricing.Input)
	require.Equal(t, 600, models[0].MaxAttachments)
	require.True(t, models[0].Capabilities.Vision)
	require.True(t, models[0].CanReason())
	require.Equal(t, []string{"low", "medium", "high", "max"}, models[0].ReasoningEffortLevels())
	require.Equal(t, "medium", models[0].Reasoning.DefaultEffortLevel)

	require.Equal(t, "claude-next-6", models[1].ID)
	require.Equal(t, "Claude Next 6", models[1].Name)
	require.EqualValues(t, 500000, models[1].ContextWindow)
	require.EqualValues(t, 64000, models[1].DefaultMaxTokens)
	require.False(t, models[1].Capabilities.Vision)
	require.Equal(t, catwalk.ThinkingAlways, models[1].Reasoning.Thinking)
	require.Empty(t, models[1].ReasoningEffortLevels())
}

func TestDiscoverAnthropicModels_IncompletePaginationFails(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":     []any{map[string]any{"id": "claude-a"}},
			"has_more": true,
		})
	}))
	defer srv.Close()

	_, err := DiscoverModels(context.Background(), Config{
		ID:      "anthropic",
		Type:    catwalk.TypeMessages,
		BaseURL: srv.URL,
		APIKey:  "sk-ant-test",
	}, echoResolver{})
	require.Error(t, err)
}
