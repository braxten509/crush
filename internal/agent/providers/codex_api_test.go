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

func TestCodexAPIRequest(t *testing.T) {
	for _, test := range []struct{ id, model, tier string }{
		{"gpt-6.1-sol", "gpt-6.1-sol", ""},
		{"gpt-6.1-sol-ultrafast", "gpt-6.1-sol", "ultrafast"},
		{"gpt-6-astra-ultrafast", "gpt-6-astra", "ultrafast"},
	} {
		t.Run(test.id, func(t *testing.T) {
			var received map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/v1/responses", r.URL.Path)
				require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
				require.NoError(t, json.NewDecoder(r.Body).Decode(&received))
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"resp-test","object":"response","status":"completed","output":[{"type":"message","id":"msg-test","role":"assistant","status":"completed","content":[{"type":"output_text","text":"ok","annotations":[]}]}],"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}`))
			}))
			defer server.Close()
			catalog := catwalk.Model{ID: test.id, CanReason: true, ReasoningLevels: []string{"low", "medium", "high", "xhigh", "max"}, DefaultReasoningEffort: "medium"}
			provider := config.ProviderConfig{ID: config.CodexAPIProviderID, Type: catwalk.TypeOpenAI, BaseURL: server.URL + "/v1", APIKey: "test-key", Models: []catwalk.Model{catalog}}
			selected := config.SelectedModel{Provider: provider.ID, Model: test.id, ReasoningEffort: "xhigh"}
			b := Builder{Config: config.NewTestStore(&config.Config{Options: &config.Options{}})}
			fp, err := b.Provider(provider, selected, false)
			require.NoError(t, err)
			lm, err := fp.LanguageModel(t.Context(), test.id)
			require.NoError(t, err)
			_, err = lm.Generate(t.Context(), fantasy.Call{
				Prompt:          fantasy.Prompt{fantasy.NewUserMessage("hi")},
				ProviderOptions: Options(catalog, selected, provider),
			})
			require.NoError(t, err)
			require.Equal(t, test.model, received["model"])
			require.Equal(t, "xhigh", received["reasoning"].(map[string]any)["effort"])
			if test.tier == "" {
				require.NotContains(t, received, "service_tier")
			} else {
				require.Equal(t, test.tier, received["service_tier"])
			}
		})
	}
}
