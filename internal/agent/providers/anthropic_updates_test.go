package providers

import (
	"charm.land/catwalk/v3/pkg/catwalk"
	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"encoding/json"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProgressUpdateWireIncludesDisplayWithoutEffort(t *testing.T) {
	requests := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			w.WriteHeader(400)
			return
		}
		requests <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_mock","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer server.Close()
	provider, err := anthropic.New(anthropic.WithBaseURL(server.URL), anthropic.WithAPIKey("local-fixture"))
	require.NoError(t, err)
	model, err := provider.LanguageModel(t.Context(), "claude-opus-5-5")
	require.NoError(t, err)
	selected := config.SelectedModel{Provider: "anthropic", Model: "claude-opus-5-5"}
	opts := Options(catwalk.Model{ID: selected.Model}, selected, config.ProviderConfig{ID: "anthropic", Type: catwalk.TypeMessages})
	_, err = model.Generate(t.Context(), fantasy.Call{Prompt: fantasy.Prompt{fantasy.NewUserMessage("check")}, ProviderOptions: opts})
	require.NoError(t, err)
	body := <-requests
	require.Equal(t, map[string]any{"type": "adaptive", "display": "updates"}, body["thinking"])
}
