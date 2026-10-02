package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

func renamedAbacusModel(id string) catwalk.Model {
	return catwalk.Model{ID: id, Name: "Example 2.5", CanReason: true,
		ReasoningLevels: []string{"low", "high"}, DefaultMaxTokens: 8192,
		Options: catwalk.ModelOptions{ProviderOptions: map[string]any{config.AbacusAPIFormatOption: "anthropic"}},
	}
}

func TestAbacusRefreshesRetiredIDWithoutChangingTheRequest(t *testing.T) {
	var requests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/messages", r.URL.Path)
		require.Equal(t, "test-key", r.Header.Get("X-Api-Key"))
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		requests = append(requests, body)
		if body["model"] == "old-id" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":{"type":"not_found_error","message":"Unknown model: old-id"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: message_stop\ndata: {}\n\n")
	}))
	defer server.Close()
	refreshes := 0
	client := &http.Client{Transport: &abacusTransport{
		transport: http.DefaultTransport, model: renamedAbacusModel("old-id"), currentID: "old-id",
		selected: config.SelectedModel{ReasoningEffort: "high", MaxTokens: 4096},
		refresh: func(context.Context) ([]catwalk.Model, error) {
			refreshes++
			return []catwalk.Model{renamedAbacusModel("new-id")}, nil
		},
	}}
	for range 2 {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/v1/messages", strings.NewReader(`{"model":"old-id","stream":true,"messages":[{"role":"user","content":"hello"}],"thinking":{"type":"adaptive"},"output_config":{"effort":"high"},"max_tokens":4096}`))
		require.NoError(t, err)
		request.Header.Set("X-Api-Key", "test-key")
		response, err := client.Do(request)
		require.NoError(t, err)
		data, err := io.ReadAll(response.Body)
		response.Body.Close()
		require.NoError(t, err)
		require.Equal(t, 200, response.StatusCode)
		require.Equal(t, "event: message_stop\ndata: {}\n\n", string(data))
	}
	require.Equal(t, 1, refreshes)
	require.Len(t, requests, 3)
	require.Equal(t, "old-id", requests[0]["model"])
	require.Equal(t, "new-id", requests[1]["model"])
	requests[0]["model"] = "new-id"
	require.Equal(t, requests[0], requests[1])
	require.Equal(t, requests[1], requests[2])
}

func TestAbacusReplacementRequiresUnambiguousCompatibleIdentity(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*abacusTransport, *[]catwalk.Model)
	}{
		{"still listed", func(_ *abacusTransport, models *[]catwalk.Model) {
			*models = append(*models, renamedAbacusModel("old-id"))
		}},
		{"different model", func(_ *abacusTransport, models *[]catwalk.Model) { (*models)[0].Name = "Example 3" }},
		{"ambiguous", func(_ *abacusTransport, models *[]catwalk.Model) {
			*models = append(*models, renamedAbacusModel("other-id"))
		}},
		{"different protocol", func(_ *abacusTransport, models *[]catwalk.Model) {
			(*models)[0].Options.ProviderOptions[config.AbacusAPIFormatOption] = "openai"
		}},
		{"unsupported effort", func(_ *abacusTransport, models *[]catwalk.Model) { (*models)[0].ReasoningLevels = []string{"low"} }},
		{"lower token limit", func(_ *abacusTransport, models *[]catwalk.Model) { (*models)[0].DefaultMaxTokens = 1024 }},
		{"no known name", func(transport *abacusTransport, _ *[]catwalk.Model) { transport.model.Name = "" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport := &abacusTransport{model: renamedAbacusModel("old-id"), selected: config.SelectedModel{ReasoningEffort: "high", MaxTokens: 4096}}
			models := []catwalk.Model{renamedAbacusModel("new-id")}
			test.change(transport, &models)
			require.Empty(t, transport.replacement(models, "old-id"))
		})
	}
}

func TestAbacusDoesNotHideOrReplayOtherFailures(t *testing.T) {
	for _, test := range []struct {
		name, body        string
		status, refreshes int
	}{
		{"invalid effort", `{"error":{"message":"Unsupported effort"}}`, 400, 0},
		{"authentication", `{"error":{"message":"Unknown model or invalid key"}}`, 401, 0},
		{"accepted stream", "event: error\ndata: {}\n\n", 200, 0},
		{"refresh unavailable", `{"error":{"message":"Unknown model: old-id"}}`, 404, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls, refreshes := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			client := &http.Client{Transport: &abacusTransport{
				transport: http.DefaultTransport, model: renamedAbacusModel("old-id"), currentID: "old-id",
				refresh: func(context.Context) ([]catwalk.Model, error) { refreshes++; return nil, errors.New("offline") },
			}}
			request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL, strings.NewReader(`{"model":"old-id"}`))
			require.NoError(t, err)
			response, err := client.Do(request)
			require.NoError(t, err)
			body, err := io.ReadAll(response.Body)
			response.Body.Close()
			require.NoError(t, err)
			require.Equal(t, test.status, response.StatusCode)
			require.Equal(t, test.body, string(body))
			require.Equal(t, 1, calls)
			require.Equal(t, test.refreshes, refreshes)
		})
	}
}
