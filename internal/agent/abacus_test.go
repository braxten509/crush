package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/openaicompat"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

func TestAbacusNativeRequestsCarryModelControls(t *testing.T) {
	for _, test := range []struct {
		id, path, effort, tier string
		renamed                bool
	}{
		{"claude-opus-5-5", "/v1/messages", "max", "", false},
		{"gpt-6.1-sol", "/v1/responses", "xhigh", "fast", false},
		{"gpt-5.3-codex", "/v1/responses", "high", "", false},
		{"gemini-3.8-flash", "/v1/chat/completions", "low", "", false},
		{"claude-opus-5-5-thinking", "/v1/messages", "max", "", true},
		{"gpt-6.1-sol", "/v1/responses", "xhigh", "fast", true},
		{"gemini-3.8-flash", "/v1/chat/completions", "low", "", true},
	} {
		t.Run(test.id, func(t *testing.T) {
			var received map[string]any
			modelID := test.id
			if test.renamed {
				modelID += "-current"
			}
			name := test.id + " display name"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/models" {
					format := map[string]string{"/v1/messages": "anthropic", "/v1/responses": "responses", "/v1/chat/completions": "openai"}[test.path]
					require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{
						"id": modelID, "display_name": name, "model_type": "text_generation", "tools": true,
						"api_formats": []string{format}, "reasoning_levels": []string{test.effort}, "max_completion_tokens": 128000,
					}}}))
					return
				}
				require.Equal(t, test.path, r.URL.Path)
				require.NoError(t, json.NewDecoder(r.Body).Decode(&received))
				if test.renamed && received["model"] == test.id {
					w.WriteHeader(http.StatusNotFound)
					_, _ = w.Write([]byte(`{"error":{"message":"Unknown model"}}`))
					return
				}
				w.Header().Set("Content-Type", "application/json")
				switch test.path {
				case "/v1/messages":
					require.Equal(t, "test-key", r.Header.Get("X-Api-Key"))
					_, _ = w.Write([]byte(`{"id":"msg-test","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"` + test.id + `","stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":2}}`))
				case "/v1/responses":
					require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
					_, _ = w.Write([]byte(`{"id":"resp-test","object":"response","status":"completed","output":[{"type":"message","id":"msg-test","role":"assistant","status":"completed","content":[{"type":"output_text","text":"ok","annotations":[]}]}],"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}`))
				default:
					_, _ = w.Write([]byte(`{"id":"chat-test","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`))
				}
			}))
			defer server.Close()
			catalog := catwalk.Model{ID: test.id, Name: name, CanReason: true, ReasoningLevels: []string{test.effort}, DefaultMaxTokens: 128000}
			provider := config.ProviderConfig{ID: config.AbacusProviderID, Type: catwalk.TypeOpenAICompat, BaseURL: server.URL + "/v1", APIKey: "test-key", Models: []catwalk.Model{catalog}}
			selected := config.SelectedModel{Provider: provider.ID, Model: test.id, ReasoningEffort: test.effort, ServiceTier: test.tier}
			c := &coordinator{cfg: config.NewTestStore(&config.Config{Options: &config.Options{}})}
			fp, err := c.buildProvider(provider, selected, false)
			require.NoError(t, err)
			lm, err := fp.LanguageModel(t.Context(), test.id)
			require.NoError(t, err)
			maxTokens := int64(8192)
			response, err := lm.Generate(t.Context(), fantasy.Call{
				Prompt: fantasy.Prompt{fantasy.NewUserMessage("hi")}, MaxOutputTokens: &maxTokens,
				ProviderOptions: getProviderOptions(Model{ModelCfg: selected, CatwalkCfg: catalog}, provider),
			})
			require.NoError(t, err)
			require.Equal(t, int64(2), response.Usage.OutputTokens)
			require.Equal(t, modelID, received["model"])
			switch test.path {
			case "/v1/messages":
				require.Equal(t, test.effort, received["output_config"].(map[string]any)["effort"])
				require.Equal(t, "adaptive", received["thinking"].(map[string]any)["type"])
				require.Equal(t, float64(8192), received["max_tokens"])
			case "/v1/responses":
				require.Equal(t, test.effort, received["reasoning"].(map[string]any)["effort"])
				require.Equal(t, float64(8192), received["max_output_tokens"])
				if test.tier == "fast" {
					require.Equal(t, "priority", received["service_tier"])
				}
			default:
				require.Equal(t, test.effort, received["reasoning_effort"])
			}
		})
	}
}

func TestAbacusThinkingTogglesAndExtraBody(t *testing.T) {
	for _, id := range []string{"claude-haiku-4-5-20251001", "deepseek-ai/DeepSeek-V3.2", "MiniMaxAI/MiniMax-M2.7", "MiniMaxAI/MiniMax-M3"} {
		for _, think := range []bool{false, true} {
			catalog := catwalk.Model{ID: id, CanReason: true}
			provider := config.ProviderConfig{ID: config.AbacusProviderID, Type: catwalk.TypeOpenAICompat, ExtraBody: map[string]any{"custom_parameter": "preserved"}}
			options := getProviderOptions(Model{CatwalkCfg: catalog, ModelCfg: config.SelectedModel{Provider: provider.ID, Model: id, Think: think}}, provider)
			if native, ok := options[anthropic.Name].(*anthropic.ProviderOptions); ok {
				require.Equal(t, think, native.Thinking != nil)
				require.Equal(t, "preserved", native.ExtraBody["custom_parameter"])
			} else {
				compatible := options[openaicompat.Name].(*openaicompat.ProviderOptions)
				mode := compatible.ExtraBody["thinking"].(map[string]any)["type"]
				if !think {
					require.Equal(t, "disabled", mode)
				} else if id == "MiniMaxAI/MiniMax-M3" {
					require.Equal(t, "adaptive", mode)
				} else {
					require.Equal(t, "enabled", mode)
				}
				require.Equal(t, "preserved", compatible.ExtraBody["custom_parameter"])
			}
		}
	}
}
