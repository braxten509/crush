package discover

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/stretchr/testify/require"
)

func TestAbacusDiscoverySupportsCodingModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/models", r.URL.Path)
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"data":[
		 {"id":"gpt-6.1-sol","display_name":"GPT-6.1 Sol","model_type":"text_generation","api_formats":["openai","responses"],"tools":true,"thinking":true,"input_modalities":["text","image"],"context_length":1000000,"max_completion_tokens":128000,"input_token_rate":"0.000002","output_token_rate":"0.00001","cached_input_token_rate":"0.0000001","reasoning_levels":["low","medium","high","xhigh","max"]},
		 {"id":"route-llm","model_type":"text_generation","api_formats":["openai"],"tools":true,"context_length":null},
		 {"id":"flux2","model_type":"image_generation","output_modalities":["image"]},
		 {"id":"responses-only","model_type":"text_generation","api_formats":["responses"],"tools":true},
		 {"id":"chat-only","model_type":"text_generation","api_formats":["openai"],"tools":false}
		]}`))
	}))
	defer server.Close()
	models, err := DiscoverModels(context.Background(), Config{ID: "abacus", BaseURL: server.URL + "/v1", APIKey: "test-key"}, &mockResolver{})
	require.NoError(t, err)
	require.Len(t, models, 3)
	require.Equal(t, "GPT-6.1 Sol", models[0].Name)
	require.Equal(t, int64(1_000_000), models[0].ContextWindow)
	require.Equal(t, int64(128_000), models[0].DefaultMaxTokens)
	require.True(t, models[0].SupportsImages)
	require.True(t, models[0].CanReason)
	require.InDelta(t, 2, models[0].CostPer1MIn, 0.000001)
	require.InDelta(t, 10, models[0].CostPer1MOut, 0.000001)
	require.InDelta(t, 0.1, models[0].CostPer1MInCached, 0.000001)
	require.Equal(t, int64(200_000), models[1].ContextWindow)
	require.Equal(t, "responses", models[0].Options.ProviderOptions["abacus_api_format"])
	require.Equal(t, "responses-only", models[2].ID)
	require.Equal(t, []string{"low", "medium", "high", "xhigh", "max"}, models[0].ReasoningLevels)
}

func TestAbacusClaudeAndGeminiEffortProfiles(t *testing.T) {
	for _, test := range []struct {
		id            string
		levels        []string
		defaultEffort string
	}{
		{"claude-opus-5-5-thinking", []string{"low", "medium", "high", "xhigh", "max"}, "medium"},
		{"claude-sonnet-5-5-thinking", []string{"low", "medium", "high", "xhigh", "max"}, "high"},
		{"claude-opus-4-6", []string{"low", "medium", "high", "max"}, "high"},
		{"gemini-3.8-flash", []string{"low", "medium", "high"}, "medium"},
	} {
		t.Run(test.id, func(t *testing.T) {
			raw := []byte(`{"id":"` + test.id + `","model_type":"text_generation","api_formats":["openai","anthropic"],"tools":true}`)
			// Native catalogue metadata is supplied by config loading, even
			// when the native provider itself is disabled.
			model, ok := abacusModel(raw, catwalk.Provider{ID: "anthropic", Models: []catwalk.Model{{
				ID: strings.TrimSuffix(test.id, "-thinking"), CanReason: true,
				ReasoningLevels: test.levels, DefaultReasoningEffort: test.defaultEffort,
			}}})
			require.True(t, ok)
			require.Equal(t, test.levels, model.ReasoningLevels)
			require.Equal(t, test.defaultEffort, model.DefaultReasoningEffort)
		})
	}
}

func TestAbacusControlsUseTheExactCurrentModel(t *testing.T) {
	provider := catwalk.Provider{ID: "openai", Models: []catwalk.Model{
		{ID: "gpt-6-astra", CanReason: true, ReasoningLevels: []string{"low", "high", "max"}},
		{ID: "gpt-6-sol", CanReason: true, ReasoningLevels: []string{"none", "low", "high"}, DefaultReasoningEffort: "high"},
	}}
	model, ok := abacusModel([]byte(`{"id":"gpt-6-sol","model_type":"text_generation","api_formats":["responses"],"tools":true}`), provider)
	require.True(t, ok)
	require.Equal(t, []string{"none", "low", "high"}, model.ReasoningLevels)
	require.Equal(t, "high", model.DefaultReasoningEffort)
	unknown, ok := abacusModel([]byte(`{"id":"gpt-6-new","model_type":"text_generation","api_formats":["responses"],"tools":true}`), provider)
	require.True(t, ok)
	require.Empty(t, unknown.ReasoningLevels, "a new model must not inherit a sibling's controls")
	declared, ok := abacusModel([]byte(`{"id":"gpt-6-sol","model_type":"text_generation","api_formats":["responses"],"tools":true,"reasoning_levels":["medium"],"default_reasoning_effort":"medium"}`), provider)
	require.True(t, ok)
	require.Equal(t, []string{"medium"}, declared.ReasoningLevels)
	require.Equal(t, "medium", declared.DefaultReasoningEffort)
}

func TestAbacusNamedModelsDoNotChangeTheirAPIIdentifier(t *testing.T) {
	provider := catwalk.Provider{ID: "deepseek", Models: []catwalk.Model{{
		ID: "deepseek-v4-pro", Name: "DeepSeek-V4-Pro", CanReason: true,
		ReasoningLevels: []string{"low", "high", "max"}, DefaultReasoningEffort: "high",
	}}}
	model, ok := abacusModel([]byte(`{"id":"deepseek-ai/DeepSeek-V4-Pro-0813","display_name":"Deepseek V4 Pro","model_type":"text_generation","api_formats":["openai"],"tools":true}`), provider)
	require.True(t, ok)
	require.Equal(t, "deepseek-ai/DeepSeek-V4-Pro-0813", model.ID)
	require.Equal(t, []string{"low", "high", "max"}, model.ReasoningLevels)
	provider.Models = append(provider.Models, catwalk.Model{ID: "different-version", Name: "Deepseek V4 Pro", ReasoningLevels: []string{"medium"}})
	model, ok = abacusModel([]byte(`{"id":"unknown-alias","display_name":"Deepseek V4 Pro","model_type":"text_generation","api_formats":["openai"],"tools":true}`), provider)
	require.True(t, ok)
	require.Empty(t, model.ReasoningLevels, "ambiguous display names cannot supply controls")
}
