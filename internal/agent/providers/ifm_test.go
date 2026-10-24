package providers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

func TestIFMRequest(t *testing.T) {
	for _, effort := range []string{"", "low", "medium", "high"} {
		t.Run("effort="+effort, func(t *testing.T) {
			var received map[string]any
			var path, authorization string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				path, authorization = r.URL.Path, r.Header.Get("Authorization")
				if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"ifm-test","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok","reasoning_content":"Thinking trace"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`))
			}))
			defer server.Close()
			model := catwalk.Model{ID: "IFM/K2-Horizon-375B-A23B", CanReason: true, ReasoningLevels: []string{"low", "medium", "high"}, DefaultReasoningEffort: "high"}
			provider := config.ProviderConfig{ID: "ifm", Type: catwalk.TypeOpenAICompat, BaseURL: server.URL + "/v1", APIKey: "test-key"}
			selected := config.SelectedModel{Provider: "ifm", Model: model.ID, ReasoningEffort: effort, ProviderOptions: map[string]any{
				"extra_body": map[string]any{"chat_template_kwargs": map[string]any{"tool_call_format": "json"}},
			}}
			builder := Builder{Config: config.NewTestStore(&config.Config{Options: &config.Options{}})}
			built, err := builder.Provider(provider, selected, false)
			require.NoError(t, err)
			languageModel, err := built.LanguageModel(t.Context(), model.ID)
			require.NoError(t, err)
			response, err := languageModel.Generate(t.Context(), fantasy.Call{
				Prompt:          fantasy.Prompt{fantasy.NewUserMessage("hi")},
				ProviderOptions: Options(model, selected, provider),
			})
			require.NoError(t, err)
			require.Equal(t, "/v1/chat/completions", path)
			require.Equal(t, "Bearer test-key", authorization)
			require.Equal(t, model.ID, received["model"])
			kwargs, ok := received["chat_template_kwargs"].(map[string]any)
			require.True(t, ok, "thinking must use IFM's chat_template_kwargs: %v", received)
			wantEffort := effort
			if wantEffort == "" {
				wantEffort = "high"
			}
			require.Equal(t, wantEffort, kwargs["reasoning_effort"])
			require.Equal(t, "json", kwargs["tool_call_format"])
			require.Equal(t, "ok", response.Content.Text())
			require.Equal(t, "Thinking trace", response.Content.ReasoningText())
		})
	}
}
