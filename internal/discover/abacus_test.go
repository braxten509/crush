package discover

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAbacusDiscoverySupportsCodingModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/models", r.URL.Path)
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"data":[
		 {"id":"gpt-6.1-sol","display_name":"GPT-6.1 Sol","model_type":"text_generation","api_formats":["openai","responses"],"tools":true,"thinking":true,"input_modalities":["text","image"],"context_length":1000000,"max_completion_tokens":128000,"input_token_rate":"0.000002","output_token_rate":"0.00001","cached_input_token_rate":"0.0000001"},
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
			model, ok := abacusModel(raw)
			require.True(t, ok)
			require.Equal(t, test.levels, model.ReasoningLevels)
			require.Equal(t, test.defaultEffort, model.DefaultReasoningEffort)
		})
	}
}
