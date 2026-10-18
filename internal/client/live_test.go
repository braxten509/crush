package client_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/client"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/proto/prototest"
	"github.com/charmbracelet/crush/internal/server"
	"github.com/stretchr/testify/require"
)

// helperModel is a fake OpenAI-compatible model for one helper: a turn whose
// prompt is helperPrompt asks for a helper (a bash call that drops the
// request into $CRUSH_TASKS_DIR) and is then held as "parent"; the helper's
// own turn is held as "helper". Calls without tools (titles) and any other
// prompt get a short answer.
type helperModel struct {
	srv  *httptest.Server
	done chan struct{}

	mu   sync.Mutex
	held map[string]int
}

const (
	helperPrompt     = "client:helper"
	helperHoldPrompt = "client:hold"
)

func startHelperModel(t *testing.T) *helperModel {
	t.Helper()
	m := &helperModel{done: make(chan struct{}), held: map[string]int{}}
	m.srv = httptest.NewServer(http.HandlerFunc(m.serve))
	t.Cleanup(func() {
		close(m.done)
		m.srv.CloseClientConnections()
		m.srv.Close()
	})
	return m
}

func (m *helperModel) serve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
		Tools []json.RawMessage `json:"tools"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	prompt, afterTools := "", false
	for _, msg := range req.Messages {
		switch msg.Role {
		case "user":
			prompt, afterTools = string(msg.Content), false
		case "tool":
			afterTools = true
		}
	}
	w.Header().Set("Content-Type", "text/event-stream")
	switch {
	case len(req.Tools) > 0 && strings.Contains(prompt, helperHoldPrompt):
		m.hold(w, r, "helper")
	case len(req.Tools) > 0 && strings.Contains(prompt, helperPrompt) && afterTools:
		m.hold(w, r, "parent")
	case len(req.Tools) > 0 && strings.Contains(prompt, helperPrompt):
		request := fmt.Sprintf(`{"session":"%%s","cli":"fake","prompt":"%s","name":"client-helper"}`, helperHoldPrompt)
		args, _ := json.Marshal(map[string]any{
			"description": "Ask for a helper",
			"command":     fmt.Sprintf(`printf '%s' "$CRUSH_SESSION_ID" > "$CRUSH_TASKS_DIR/client.tmp" && mv "$CRUSH_TASKS_DIR/client.tmp" "$CRUSH_TASKS_DIR/client.req"`, request),
		})
		helperChunk(w, map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{
			"index": 0, "id": "call_helper", "type": "function",
			"function": map[string]any{"name": "bash", "arguments": string(args)},
		}}}, nil)
		helperEnd(w, "tool_calls")
	default:
		helperChunk(w, map[string]any{"role": "assistant", "content": "Noted."}, nil)
		helperEnd(w, "stop")
	}
}

func (m *helperModel) hold(w http.ResponseWriter, r *http.Request, name string) {
	m.mu.Lock()
	m.held[name]++
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.held[name]--
		m.mu.Unlock()
	}()
	w.WriteHeader(http.StatusOK)
	helperChunk(w, map[string]any{"role": "assistant", "content": "Working"}, nil)
	if fl, ok := w.(http.Flusher); ok {
		fl.Flush()
	}
	select {
	case <-r.Context().Done():
	case <-m.done:
	}
}

func (m *helperModel) heldNow(name string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.held[name]
}

func (m *helperModel) waitHeld(t *testing.T, name string, n int) {
	t.Helper()
	require.Eventually(t, func() bool { return m.heldNow(name) == n }, 20*time.Second, 10*time.Millisecond,
		"model calls held for %s: have %d, want %d", name, m.heldNow(name), n)
}

func helperChunk(w http.ResponseWriter, delta map[string]any, finish any) {
	data, _ := json.Marshal(map[string]any{
		"id": "helper", "object": "chat.completion.chunk", "created": 1, "model": "fake-model",
		"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}},
	})
	fmt.Fprintf(w, "data: %s\n\n", data)
}

func helperEnd(w http.ResponseWriter, finish string) {
	helperChunk(w, map[string]any{}, finish)
	data, _ := json.Marshal(map[string]any{
		"id": "helper", "object": "chat.completion.chunk", "created": 1, "model": "fake-model",
		"choices": []any{}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 2, "total_tokens": 12},
	})
	fmt.Fprintf(w, "data: %s\n\n", data)
	fmt.Fprint(w, "data: [DONE]\n\n")
}

// TestClientHelpersRoundTrip: ListHelpers and StopHelper against the real
// handler. A real helper is listed with its fields, a stop through another
// chat is refused with the server's message and leaves it running, and the
// stop through its own chat ends it (engine-design.md 5.3).
func TestClientHelpersRoundTrip(t *testing.T) {
	model := startHelperModel(t)
	for _, name := range []string{"HOME", "XDG_CACHE_HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME"} {
		t.Setenv(name, t.TempDir())
	}
	t.Setenv("CRUSH_DISABLE_PROVIDER_AUTO_UPDATE", "1")
	dir := t.TempDir()
	cfg, err := json.Marshal(map[string]any{
		"providers": map[string]any{"fake": map[string]any{
			"type": "openai-compat", "base_url": model.srv.URL + "/v1", "api_key": "test-key",
			"models": []map[string]any{{"id": "fake-model", "name": "Fake model", "context_window": 200000, "default_max_tokens": 256}},
		}},
		"models": map[string]any{
			"large": map[string]any{"provider": "fake", "model": "fake-model"},
			"small": map[string]any{"provider": "fake", "model": "fake-model"},
		},
		"options": map[string]any{
			"disable_auto_summarize": true, "disable_metrics": true, "disable_provider_auto_update": true,
			"auto_lsp": false, "notifications": "disabled",
		},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "crush.json"), cfg, 0o600))

	s := server.NewServer(nil, "tcp", "127.0.0.1:0")
	hs := httptest.NewServer(s.Handler())
	t.Cleanup(hs.Close)
	u, err := url.Parse(hs.URL)
	require.NoError(t, err)
	c, err := client.NewClient(dir, "tcp", u.Host)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.RetireClient(context.Background()) })

	ctx := t.Context()
	ws, err := c.CreateWorkspace(ctx, prototest.CreateRequest(proto.Workspace{Path: dir, DataDir: t.TempDir(), YOLO: true}))
	require.NoError(t, err)
	require.NoError(t, c.InitiateAgentProcessing(ctx, ws.ID, true))
	chat, err := c.CreateSession(ctx, ws.ID, "chat")
	require.NoError(t, err)
	other, err := c.CreateSession(ctx, ws.ID, "other")
	require.NoError(t, err)

	helpers, err := c.ListHelpers(ctx, ws.ID, chat.ID)
	require.NoError(t, err)
	require.Empty(t, helpers)

	require.NoError(t, c.SendMessage(ctx, ws.ID, chat.ID, "", "", helperPrompt))
	model.waitHeld(t, "helper", 1)
	var helper proto.Helper
	require.Eventually(t, func() bool {
		helpers, err := c.ListHelpers(ctx, ws.ID, chat.ID)
		if err != nil || len(helpers) != 1 {
			return false
		}
		helper = helpers[0]
		return true
	}, 20*time.Second, 20*time.Millisecond, "the helper is listed in its chat")
	require.NotEmpty(t, helper.ID)
	require.Equal(t, chat.ID, helper.SessionID)
	require.NotEmpty(t, helper.ChildSessionID)
	require.Equal(t, "client-helper", helper.Name)
	require.Equal(t, "fake", helper.CLI)
	require.Equal(t, "running", helper.Status)
	require.False(t, helper.Started.IsZero())
	require.Equal(t, c.ClientID(), helper.StartedBy)

	err = c.StopHelper(ctx, ws.ID, other.ID, helper.ID)
	require.ErrorIs(t, err, client.ErrNotFound)
	require.ErrorContains(t, err, "helper not found in this chat")
	time.Sleep(200 * time.Millisecond)
	require.Equal(t, 1, model.heldNow("helper"), "a refused stop leaves the helper running")

	require.NoError(t, c.StopHelper(ctx, ws.ID, chat.ID, helper.ID))
	model.waitHeld(t, "helper", 0)
	helpers, err = c.ListHelpers(ctx, ws.ID, chat.ID)
	require.NoError(t, err)
	require.Len(t, helpers, 1)
	require.Equal(t, "stopped", helpers[0].Status)
}
