package discover

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/stretchr/testify/require"
)

func TestGeminiDiscoveryListsChatModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1beta/models", r.URL.Path)
		require.Equal(t, "test-key", r.Header.Get("x-goog-api-key"))
		require.Empty(t, r.Header.Get("Authorization"))
		switch r.URL.Query().Get("pageToken") {
		case "":
			_, _ = w.Write([]byte(`{"models":[
			 {"name":"models/gemini-9-new","displayName":"Gemini 9 New","inputTokenLimit":2000000,"outputTokenLimit":65536,"thinking":true,"supportedGenerationMethods":["generateContent","countTokens"]},
			 {"name":"models/gemini-3.8-flash","displayName":"Gemini 3.8 Flash","inputTokenLimit":1048576,"outputTokenLimit":65536,"thinking":true,"supportedGenerationMethods":["generateContent"]},
			 {"name":"models/gemini-9-flash-tts","displayName":"Gemini 9 Flash TTS","thinking":null,"supportedGenerationMethods":["generateContent"]},
			 {"name":"models/gemini-9-flash-image","displayName":"Nano Banana 9","description":"Gemini 9 Flash Image.","thinking":true,"supportedGenerationMethods":["generateContent"]},
			 {"name":"models/gemini-9-live","displayName":"Gemini 9 Live","thinking":true,"supportedGenerationMethods":["bidiGenerateContent"]},
			 {"name":"models/gemini-embedding-9","thinking":null,"supportedGenerationMethods":["embedContent"]}
			],"nextPageToken":"page-2"}`))
		case "page-2":
			_, _ = w.Write([]byte(`{"models":[
			 {"name":"models/gemini-2.9-pro","displayName":"Gemini 2.9 Pro","inputTokenLimit":1048576,"outputTokenLimit":65536,"thinking":true,"supportedGenerationMethods":["generateContent"]},
			 {"name":"models/mine","displayName":"Listed Again","thinking":true,"supportedGenerationMethods":["generateContent"]}
			]}`))
		default:
			t.Fatalf("unexpected page token %q", r.URL.Query().Get("pageToken"))
		}
	}))
	defer server.Close()

	catalog := []catwalk.Provider{{
		ID:   catwalk.InferenceProviderGemini,
		Type: catwalk.TypeGoogle,
		Models: []catwalk.Model{{
			ID: "gemini-3.8-flash", Name: "Gemini 3.8 Flash", CostPer1MIn: 0.5, CostPer1MOut: 3,
			CanReason: true, ReasoningLevels: []string{"low", "medium", "high"}, DefaultReasoningEffort: "high",
		}},
	}}
	models, err := DiscoverModels(context.Background(), Config{
		ID:             "gemini",
		Type:           catwalk.TypeGoogle,
		BaseURL:        server.URL + "/v1beta/",
		APIKey:         "test-key",
		ExistingModels: []catwalk.Model{{ID: "mine", Name: "Mine"}},
		KnownProviders: catalog,
	}, &mockResolver{})
	require.NoError(t, err)

	ids := make([]string, len(models))
	for i, model := range models {
		ids[i] = model.ID
	}
	require.Equal(t, []string{"mine", "gemini-9-new", "gemini-3.8-flash", "gemini-2.9-pro"}, ids)
	require.Equal(t, "Mine", models[0].Name)

	newModel := models[1]
	require.Equal(t, "Gemini 9 New", newModel.Name)
	require.Equal(t, int64(2_000_000), newModel.ContextWindow)
	require.Equal(t, int64(65536), newModel.DefaultMaxTokens)
	require.True(t, newModel.CanReason)
	require.True(t, newModel.SupportsImages)
	require.Equal(t, []string{"low", "medium", "high"}, newModel.ReasoningLevels)
	require.Equal(t, "medium", newModel.DefaultReasoningEffort)

	knownModel := models[2]
	require.InDelta(t, 0.5, knownModel.CostPer1MIn, 0.000001)
	require.Equal(t, "high", knownModel.DefaultReasoningEffort)
	require.Equal(t, int64(1048576), knownModel.ContextWindow)

	budgetModel := models[3]
	require.True(t, budgetModel.CanReason)
	require.Empty(t, budgetModel.ReasoningLevels)
}

func TestGeminiDiscoveryReportsHTTPErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	_, err := DiscoverModels(context.Background(), Config{ID: "gemini", Type: catwalk.TypeGoogle, BaseURL: server.URL, APIKey: "bad"}, &mockResolver{})
	require.ErrorContains(t, err, "403")
}
