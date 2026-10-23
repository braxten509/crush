package providers

import (
	"context"
	"slices"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/bedrock"
	"charm.land/fantasy/providers/openaicompat"
	"charm.land/fantasy/providers/openrouter"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/discover"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopilotResponsesModels(t *testing.T) {
	t.Parallel()

	for _, modelID := range []string{
		"gpt-6-astra",
		"gpt-6-luna",
		"gpt-6-sol",
		"gpt-6.1-sol",
		"grok-4.5",
		"grok-4.6",
		"grok-4.7",
	} {
		assert.True(t, copilotResponsesModels[modelID], modelID)
	}
	assert.False(t, copilotResponsesModels["gpt-4.1"])
}

func TestGetProviderOptionsReasoningEffort(t *testing.T) {
	// Bedrock is Fantasy's Anthropic under a different provider name; options
	// must land under anthropic.Name so the Anthropic language model picks them up.
	tests := []struct {
		name         string
		providerType catwalk.Type
	}{
		{"anthropic honors reasoning_effort", catwalk.Type(anthropic.Name)},
		{"bedrock honors reasoning_effort", catwalk.Type(bedrock.Name)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			model := modelConfig{
				CatwalkCfg: catwalk.Model{
					ID:              "claude-opus-4-7",
					CanReason:       true,
					ReasoningLevels: []string{"max"},
				},
				ModelCfg: config.SelectedModel{
					Provider:        "test",
					ReasoningEffort: "max",
				},
			}
			providerCfg := config.ProviderConfig{ID: "test", Type: tc.providerType}

			opts := Options(model.CatwalkCfg, model.ModelCfg, providerCfg)

			raw, ok := opts[anthropic.Name]
			require.True(t, ok, "options should be keyed under anthropic.Name for type %q", tc.providerType)
			parsed, ok := raw.(*anthropic.ProviderOptions)
			require.True(t, ok)
			require.NotNil(t, parsed.Effort)
			assert.Equal(t, anthropic.Effort("max"), *parsed.Effort)
		})
	}
}

func TestGetProviderOptionsReasoningEffortCustomProvider(t *testing.T) {
	// Custom local providers (lmstudio, ollama, omlx, litellm, llamacpp)
	// go through the OpenAI-compat client and must receive the selected
	// reasoning effort like any other OpenAI-compatible provider.
	for _, providerType := range discover.RegisteredProviderTypes() {
		t.Run(providerType, func(t *testing.T) {
			model := modelConfig{
				CatwalkCfg: catwalk.Model{
					ID:              "qwen/qwen3-8b",
					CanReason:       true,
					ReasoningLevels: []string{"low", "medium", "high"},
				},
				ModelCfg: config.SelectedModel{
					Provider:        "local",
					Model:           "qwen/qwen3-8b",
					ReasoningEffort: "high",
				},
			}
			providerCfg := config.ProviderConfig{ID: "local", Type: catwalk.Type(providerType)}

			opts := Options(model.CatwalkCfg, model.ModelCfg, providerCfg)

			raw, ok := opts[openaicompat.Name]
			require.True(t, ok, "options should be keyed under openaicompat.Name for type %q", providerType)
			parsed, ok := raw.(*openaicompat.ProviderOptions)
			require.True(t, ok)
			require.NotNil(t, parsed.ReasoningEffort)
			assert.Equal(t, "high", string(*parsed.ReasoningEffort))
		})
	}
}

func TestGetProviderOptionsReasoningEffortFallback(t *testing.T) {
	model := modelConfig{
		CatwalkCfg: catwalk.Model{
			ID:              "glm-5.2",
			CanReason:       true,
			ReasoningLevels: []string{"high", "max"},
		},
		ModelCfg: config.SelectedModel{
			Provider: "zai",
		},
	}
	providerCfg := config.ProviderConfig{
		ID:   string(catwalk.InferenceProviderZAI),
		Type: openaicompat.Name,
	}

	opts := Options(model.CatwalkCfg, model.ModelCfg, providerCfg)

	raw, ok := opts[openaicompat.Name]
	require.True(t, ok)
	parsed, ok := raw.(*openaicompat.ProviderOptions)
	require.True(t, ok)
	require.NotNil(t, parsed.ReasoningEffort)
	assert.Equal(t, "high", string(*parsed.ReasoningEffort))

	thinking, ok := parsed.ExtraBody["thinking"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "enabled", thinking["type"])
}

func TestGetProviderOptionsTopKExtraBody(t *testing.T) {
	// "ollama" has a registered discover.Enricher, so it is treated as a
	// known custom provider speaking openai-compat.
	knownCustomProviderCfg := config.ProviderConfig{ID: "ollama", Type: "ollama"}

	t.Run("model top_k is injected into extra_body for known custom providers", func(t *testing.T) {
		model := modelConfig{
			CatwalkCfg: catwalk.Model{ID: "llama3"},
			ModelCfg:   config.SelectedModel{Provider: "ollama", TopK: ptr(int64(40))},
		}

		opts := Options(model.CatwalkCfg, model.ModelCfg, knownCustomProviderCfg)

		raw, ok := opts[openaicompat.Name]
		require.True(t, ok)
		parsed, ok := raw.(*openaicompat.ProviderOptions)
		require.True(t, ok)
		topK, ok := parsed.ExtraBody["top_k"].(int64)
		require.True(t, ok)
		assert.Equal(t, int64(40), topK)
	})

	t.Run("falls back to catwalk top_k when the model config has none", func(t *testing.T) {
		model := modelConfig{
			CatwalkCfg: catwalk.Model{
				ID:      "llama3",
				Options: catwalk.ModelOptions{TopK: ptr(int64(64))},
			},
			ModelCfg: config.SelectedModel{Provider: "ollama"},
		}

		opts := Options(model.CatwalkCfg, model.ModelCfg, knownCustomProviderCfg)

		raw, ok := opts[openaicompat.Name]
		require.True(t, ok)
		parsed, ok := raw.(*openaicompat.ProviderOptions)
		require.True(t, ok)
		topK, ok := parsed.ExtraBody["top_k"].(int64)
		require.True(t, ok)
		assert.Equal(t, int64(64), topK)
	})

	t.Run("does not set extra_body when no top_k is configured anywhere", func(t *testing.T) {
		model := modelConfig{
			CatwalkCfg: catwalk.Model{ID: "llama3"},
			ModelCfg:   config.SelectedModel{Provider: "ollama"},
		}

		opts := Options(model.CatwalkCfg, model.ModelCfg, knownCustomProviderCfg)

		raw, ok := opts[openaicompat.Name]
		require.True(t, ok)
		parsed, ok := raw.(*openaicompat.ProviderOptions)
		require.True(t, ok)
		_, hasTopK := parsed.ExtraBody["top_k"]
		assert.False(t, hasTopK)
	})

	t.Run("does not overwrite an explicitly configured extra_body.top_k", func(t *testing.T) {
		model := modelConfig{
			CatwalkCfg: catwalk.Model{ID: "llama3"},
			ModelCfg: config.SelectedModel{
				Provider: "ollama",
				TopK:     ptr(int64(40)),
				ProviderOptions: map[string]any{
					"extra_body": map[string]any{"top_k": 7},
				},
			},
		}

		opts := Options(model.CatwalkCfg, model.ModelCfg, knownCustomProviderCfg)

		raw, ok := opts[openaicompat.Name]
		require.True(t, ok)
		parsed, ok := raw.(*openaicompat.ProviderOptions)
		require.True(t, ok)
		assert.EqualValues(t, 7, parsed.ExtraBody["top_k"])
	})

	t.Run("is not injected for providers outside the known-custom-provider default branch", func(t *testing.T) {
		model := modelConfig{
			CatwalkCfg: catwalk.Model{ID: "glm-5.2"},
			ModelCfg:   config.SelectedModel{Provider: "zai", TopK: ptr(int64(40))},
		}
		providerCfg := config.ProviderConfig{ID: string(catwalk.InferenceProviderZAI), Type: openaicompat.Name}

		opts := Options(model.CatwalkCfg, model.ModelCfg, providerCfg)

		raw, ok := opts[openaicompat.Name]
		require.True(t, ok)
		parsed, ok := raw.(*openaicompat.ProviderOptions)
		require.True(t, ok)
		_, hasTopK := parsed.ExtraBody["top_k"]
		assert.False(t, hasTopK)
	})
}

func TestGetProviderOptionsMalformedFallback(t *testing.T) {
	model := modelConfig{
		CatwalkCfg: catwalk.Model{ID: "llama3"},
		ModelCfg: config.SelectedModel{
			Provider:        "ollama",
			TopK:            ptr(int64(40)),
			ProviderOptions: map[string]any{"user": 5.0},
		},
	}
	providerCfg := config.ProviderConfig{ID: "test", Type: "ollama"}

	opts := Options(model.CatwalkCfg, model.ModelCfg, providerCfg)

	raw, ok := opts[openaicompat.Name]
	require.True(t, ok, "malformed provider_options should still fall back to top_k")
	parsed, ok := raw.(*openaicompat.ProviderOptions)
	require.True(t, ok)

	// The malformed fields are dropped; only the injected top_k survives.
	assert.Nil(t, parsed.User)
	assert.Nil(t, parsed.ReasoningEffort)
	require.Len(t, parsed.ExtraBody, 1)
	topK, ok := parsed.ExtraBody["top_k"].(int64)
	require.True(t, ok)
	assert.Equal(t, int64(40), topK)
}

func TestCallTopK(t *testing.T) {
	knownCustomProviderCfg := config.ProviderConfig{ID: "ollama", Type: "ollama"}

	tests := []struct {
		name        string
		providerCfg config.ProviderConfig
		want        *int64
	}{
		{
			name:        "suppressed for known custom providers",
			providerCfg: knownCustomProviderCfg,
			want:        nil,
		},
		{
			name:        "passed through for openai-compat hosted providers",
			providerCfg: config.ProviderConfig{ID: "zai", Type: openaicompat.Name},
			want:        ptr(int64(40)),
		},
		{
			name:        "passed through for anthropic",
			providerCfg: config.ProviderConfig{ID: "anthropic", Type: catwalk.Type(anthropic.Name)},
			want:        ptr(int64(40)),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CallTopK(tc.providerCfg, ptr(int64(40)))
			if tc.want == nil {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, *tc.want, *got)
		})
	}

	t.Run("nil input stays nil regardless of provider", func(t *testing.T) {
		assert.Nil(t, CallTopK(knownCustomProviderCfg, nil))
		assert.Nil(t, CallTopK(config.ProviderConfig{Type: openaicompat.Name}, nil))
	})
}

func ptr[T any](v T) *T { return &v }

// OpenRouter is asked for the cheapest host first unless its switch is off
// or the user's own options pick a sort or an order, and for FP8 quality or
// better (owner, 2026-10-22).
func TestOpenRouterAsksForTheCheapestHost(t *testing.T) {
	t.Parallel()
	routing := func(p config.ProviderConfig, selected config.SelectedModel) *openrouter.Provider {
		options := Options(catwalk.Model{ID: "deepseek/deepseek-v4.1-flash"}, selected, p)
		parsed, ok := options[openrouter.Name].(*openrouter.ProviderOptions)
		require.True(t, ok)
		return parsed.Provider
	}
	price := "price"
	floor := slices.Clone(openRouterQuantizations)
	p := config.ProviderConfig{ID: "openrouter", Type: openrouter.Name}
	require.Equal(t, &openrouter.Provider{Sort: &price, Quantizations: floor}, routing(p, config.SelectedModel{}))

	// The user's own routing keeps its other fields; its own sort or order wins.
	own := config.SelectedModel{ProviderOptions: map[string]any{"provider": map[string]any{"ignore": []any{"x"}}}}
	require.Equal(t, &openrouter.Provider{Sort: &price, Quantizations: floor, Ignore: []string{"x"}}, routing(p, own))
	latency := "latency"
	own = config.SelectedModel{ProviderOptions: map[string]any{"provider": map[string]any{"sort": "latency"}}}
	require.Equal(t, &openrouter.Provider{Sort: &latency, Quantizations: floor}, routing(p, own))
	own = config.SelectedModel{ProviderOptions: map[string]any{"provider": map[string]any{"order": []any{"deepseek"}}}}
	require.Equal(t, &openrouter.Provider{Order: []string{"deepseek"}, Quantizations: floor}, routing(p, own))

	off := false
	p.CheapestHost = &off
	p.QualityFloor = &off
	require.Nil(t, routing(p, config.SelectedModel{}))
}

// Once OpenRouter's hosts for the model are read, the request names them
// cheapest first, so a cheaper cache read wins a tie (owner, 2026-10-22).
func TestOpenRouterNamesItsHostsCheapestFirst(t *testing.T) {
	restore := openRouterHostOrder
	openRouterHostOrder = func(_ context.Context, baseURL, model string) []string {
		if model == "deepseek/deepseek-v4.1-flash" {
			return []string{"cheap-cache", "cheap/fp8"}
		}
		return nil
	}
	t.Cleanup(func() { openRouterHostOrder = restore })
	price := "price"
	floor := slices.Clone(openRouterQuantizations)
	p := config.ProviderConfig{ID: "openrouter", Type: openrouter.Name, BaseURL: "https://openrouter.ai/api/v1"}
	options := Options(catwalk.Model{ID: "deepseek/deepseek-v4.1-flash"}, config.SelectedModel{}, p)
	require.Equal(t, &openrouter.Provider{Order: []string{"cheap-cache", "cheap/fp8"}, Sort: &price, Quantizations: floor}, options[openrouter.Name].(*openrouter.ProviderOptions).Provider)
	// Not read yet: the plain cheapest-first sort.
	options = Options(catwalk.Model{ID: "other/model"}, config.SelectedModel{}, p)
	require.Equal(t, &openrouter.Provider{Sort: &price, Quantizations: floor}, options[openrouter.Name].(*openrouter.ProviderOptions).Provider)
}
