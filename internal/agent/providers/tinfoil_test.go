package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"charm.land/fantasy/providers/openaicompat"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

// Each Tinfoil model's thinking switch is sent the way Tinfoil's model list
// says, on every Tinfoil account.
func TestTinfoilThinking(t *testing.T) {
	body, err := os.ReadFile("../../config/testdata/tinfoil-models.json")
	require.NoError(t, err)
	models, err := config.LoadTinfoilModels(body)
	require.NoError(t, err)
	byID := map[string]catwalk.Model{}
	for _, m := range models {
		byID[m.ID] = m
	}
	kwargs := func(m map[string]any) map[string]any {
		return map[string]any{"chat_template_kwargs": m}
	}
	for _, tc := range []struct {
		name, provider, model, effort string
		think                         bool
		userBody                      map[string]any
		want                          map[string]any
	}{
		{name: "effort mapped", provider: "tinfoil", model: "deepseek-v4-1-flash", effort: "high",
			want: kwargs(map[string]any{"reasoning_effort": "xhigh", "thinking": true})},
		{name: "user body kept", provider: "tinfoil", model: "deepseek-v4-1-flash", effort: "low",
			userBody: kwargs(map[string]any{"foo": "bar"}),
			want:     kwargs(map[string]any{"reasoning_effort": "low", "thinking": true, "foo": "bar"})},
		{name: "second account", provider: "tinfoil-2", model: "glm-5-3", effort: "high",
			want: kwargs(map[string]any{"reasoning_effort": "max"})},
		{name: "think on", provider: "tinfoil", model: "gemma4-31b", think: true,
			want: kwargs(map[string]any{"enable_thinking": true})},
		{name: "think off", provider: "tinfoil", model: "gemma4-31b",
			want: kwargs(map[string]any{"enable_thinking": false})},
		{name: "plain effort", provider: "tinfoil", model: "gpt-oss-120b", effort: "low",
			want: map[string]any{"reasoning_effort": "low"}},
		{name: "always thinks", provider: "tinfoil", model: "kimi-k3", think: true,
			want: map[string]any{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			selected := config.SelectedModel{Provider: tc.provider, Model: tc.model, ReasoningEffort: tc.effort, Think: tc.think}
			if tc.userBody != nil {
				selected.ProviderOptions = map[string]any{"extra_body": tc.userBody}
			}
			provider := config.ProviderConfig{ID: tc.provider, Type: catwalk.TypeOpenAICompat, BaseURL: "https://inference.tinfoil.sh/v1"}
			raw := Options(byID[tc.model], selected, provider)[openaicompat.Name]
			parsed, ok := raw.(*openaicompat.ProviderOptions)
			require.True(t, ok)
			require.Nil(t, parsed.ReasoningEffort)
			require.Equal(t, tc.want, parsed.ExtraBody)
		})
	}
}

// A Tinfoil request goes only to a server that proves it is Tinfoil's
// secure server; a plain server never sees the prompt.
func TestTinfoilRefusesUnprovenServer(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/chat/completions" {
			hits.Add(1)
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	model := catwalk.Model{ID: "gemma4-31b"}
	provider := config.ProviderConfig{ID: "tinfoil", Type: catwalk.TypeOpenAICompat, BaseURL: server.URL + "/v1", APIKey: "test-key"}
	selected := config.SelectedModel{Provider: "tinfoil", Model: model.ID}
	builder := Builder{Config: config.NewTestStore(&config.Config{Options: &config.Options{}})}
	built, err := builder.Provider(provider, selected, false)
	require.NoError(t, err)
	languageModel, err := built.LanguageModel(t.Context(), model.ID)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	_, err = languageModel.Generate(ctx, fantasy.Call{Prompt: fantasy.Prompt{fantasy.NewUserMessage("secret")}})
	require.Error(t, err)
	require.Zero(t, hits.Load())
}
