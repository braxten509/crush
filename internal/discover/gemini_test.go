package discover

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/stretchr/testify/require"
)

func TestGeminiDiscoveryListsChatModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "test-key", r.Header.Get("x-goog-api-key"))
		require.Empty(t, r.Header.Get("Authorization"))
		if strings.HasSuffix(r.URL.Path, ":generateContent") {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		require.Equal(t, "/v1beta/models", r.URL.Path)
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

func TestGeminiDiscoveryHidesModelsGoogleRefuses(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "checks.json")
	old := geminiCheckCachePath
	geminiCheckCachePath = func() string { return cache }
	t.Cleanup(func() { geminiCheckCachePath = old })

	var mu sync.Mutex
	asked := map[string]int{}
	busy := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1beta/models" {
			_, _ = w.Write([]byte(`{"models":[
			 {"name":"models/gemini-works","thinking":true,"supportedGenerationMethods":["generateContent"]},
			 {"name":"models/gemini-retired","thinking":true,"supportedGenerationMethods":["generateContent"]},
			 {"name":"models/gemini-interactions-only","thinking":true,"supportedGenerationMethods":["generateContent"]},
			 {"name":"models/gemini-busy","thinking":true,"supportedGenerationMethods":["generateContent"]}
			]}`))
			return
		}
		require.Equal(t, http.MethodPost, r.Method)
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1beta/models/"), ":generateContent")
		mu.Lock()
		asked[id]++
		stillBusy := busy
		mu.Unlock()
		switch {
		case id == "gemini-retired":
			w.WriteHeader(http.StatusNotFound)
		case id == "gemini-interactions-only":
			w.WriteHeader(http.StatusBadRequest)
		case id == "gemini-busy" && stillBusy:
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer server.Close()

	discoverIDs := func() []string {
		models, err := DiscoverModels(context.Background(), Config{ID: "gemini", Type: catwalk.TypeGoogle, BaseURL: server.URL, APIKey: "k"}, &mockResolver{})
		require.NoError(t, err)
		ids := make([]string, len(models))
		for i, model := range models {
			ids[i] = model.ID
		}
		return ids
	}

	// A rate limit says nothing about the model: it stays listed.
	require.Equal(t, []string{"gemini-works", "gemini-busy"}, discoverIDs())

	// Known answers come from the saved checks; only the unanswered model is asked again.
	mu.Lock()
	busy = false
	mu.Unlock()
	require.Equal(t, []string{"gemini-works", "gemini-busy"}, discoverIDs())
	require.Equal(t, map[string]int{"gemini-works": 1, "gemini-retired": 1, "gemini-interactions-only": 1, "gemini-busy": 2}, asked)
	require.FileExists(t, cache)
}
