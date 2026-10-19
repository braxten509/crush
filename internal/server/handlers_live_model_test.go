package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// A fake model for route tests that need real helpers and jobs: an
// OpenAI-compatible server whose answer is picked by a marker in the latest
// prompt (the backend's live_model_test.go has the same one).
//
//   - live:hold:NAME holds the call open until it is stopped or the test
//     ends.
//   - live:job:NAME starts a background job, live:helper:NAME asks for a
//     helper whose prompt is live:hold:NAME-helper, live:spawn:NAME does
//     both. The call after their results is held as NAME.
//   - Anything else, and calls without tools (titles), get a short answer.
var liveModelMarker = regexp.MustCompile(`live:(hold|job|helper|spawn):([a-z0-9-]+)`)

type liveModel struct {
	srv  *httptest.Server
	done chan struct{}

	mu   sync.Mutex
	held map[string]int
}

// startLiveModel starts the fake model. Start it before the server under
// test, so its cleanup runs last.
func startLiveModel(t *testing.T) *liveModel {
	t.Helper()
	m := &liveModel{done: make(chan struct{}), held: map[string]int{}}
	m.srv = httptest.NewServer(http.HandlerFunc(m.serve))
	t.Cleanup(func() {
		close(m.done)
		m.srv.CloseClientConnections()
		m.srv.Close()
	})
	return m
}

func (m *liveModel) serve(w http.ResponseWriter, r *http.Request) {
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
	last := -1
	for i, msg := range req.Messages {
		if msg.Role == "user" {
			last = i
		}
	}
	var verb, name string
	afterTools := false
	if last >= 0 {
		if found := liveModelMarker.FindStringSubmatch(liveModelText(req.Messages[last].Content)); found != nil {
			verb, name = found[1], found[2]
		}
		for _, msg := range req.Messages[last+1:] {
			afterTools = afterTools || msg.Role == "tool"
		}
	}
	w.Header().Set("Content-Type", "text/event-stream")
	switch {
	case len(req.Tools) == 0 || verb == "":
		liveModelChunk(w, map[string]any{"role": "assistant", "content": "Noted."}, nil)
		liveModelEnd(w, "stop")
	case verb != "hold" && !afterTools:
		liveModelToolCalls(w, name, verb == "job" || verb == "spawn", verb == "helper" || verb == "spawn")
	default:
		m.hold(w, r, name)
	}
}

func (m *liveModel) hold(w http.ResponseWriter, r *http.Request, name string) {
	m.mu.Lock()
	m.held[name]++
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.held[name]--
		m.mu.Unlock()
	}()
	w.WriteHeader(http.StatusOK)
	liveModelChunk(w, map[string]any{"role": "assistant", "content": "Working"}, nil)
	if fl, ok := w.(http.Flusher); ok {
		fl.Flush()
	}
	select {
	case <-r.Context().Done():
	case <-m.done:
	}
}

func (m *liveModel) heldNow(name string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.held[name]
}

func (m *liveModel) waitHeld(t *testing.T, name string, n int) {
	t.Helper()
	require.Eventually(t, func() bool { return m.heldNow(name) == n }, 20*time.Second, 10*time.Millisecond,
		"model calls held for %s: have %d, want %d", name, m.heldNow(name), n)
}

func liveModelText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(raw, &parts)
	var sb strings.Builder
	for _, p := range parts {
		sb.WriteString(p.Text)
	}
	return sb.String()
}

func liveModelChunk(w http.ResponseWriter, delta map[string]any, finish any) {
	data, _ := json.Marshal(map[string]any{
		"id": "live", "object": "chat.completion.chunk", "created": 1, "model": "fake-model",
		"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}},
	})
	fmt.Fprintf(w, "data: %s\n\n", data)
}

func liveModelEnd(w http.ResponseWriter, finish string) {
	liveModelChunk(w, map[string]any{}, finish)
	data, _ := json.Marshal(map[string]any{
		"id": "live", "object": "chat.completion.chunk", "created": 1, "model": "fake-model",
		"choices": []any{}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 2, "total_tokens": 12},
	})
	fmt.Fprintf(w, "data: %s\n\n", data)
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func liveModelToolCalls(w http.ResponseWriter, name string, job, helper bool) {
	var calls [][2]string
	if job {
		command := "sleep 120"
		if strings.HasPrefix(name, "listen") {
			// A job that listens on a local port, like a dev server (G-18).
			command = `python3 -c 'import socket,time; s=socket.socket(); s.bind(("127.0.0.1", 0)); s.listen(); time.sleep(120)'`
		}
		args, _ := json.Marshal(map[string]any{
			"description": "Start the " + name + " job", "command": command, "run_in_background": true,
		})
		calls = append(calls, [2]string{"call_job_" + name, string(args)})
	}
	if helper {
		request := fmt.Sprintf(`{"session":"%%s","cli":"fake","prompt":"live:hold:%s-helper","name":"%s-helper"}`, name, name)
		args, _ := json.Marshal(map[string]any{
			"description": "Ask for the " + name + " helper",
			"command": fmt.Sprintf(`printf '%s' "$CRUSH_SESSION_ID" > "$CRUSH_TASKS_DIR/%s.tmp" && mv "$CRUSH_TASKS_DIR/%s.tmp" "$CRUSH_TASKS_DIR/%s.req"`,
				request, name, name, name),
		})
		calls = append(calls, [2]string{"call_helper_" + name, string(args)})
	}
	for i, call := range calls {
		delta := map[string]any{"tool_calls": []any{map[string]any{
			"index": i, "id": call[0], "type": "function",
			"function": map[string]any{"name": "bash", "arguments": call[1]},
		}}}
		if i == 0 {
			delta["role"] = "assistant"
		}
		liveModelChunk(w, delta, nil)
	}
	liveModelEnd(w, "tool_calls")
}

// liveWork is a server with one real workspace whose coder runs on the fake
// model, and one app on it.
type liveWork struct {
	h     *e2eHarness
	model *liveModel
	ws    proto.Workspace
	app   string
}

// newLiveWork creates the workspace through the HTTP route and starts the
// app's agent.
func newLiveWork(t *testing.T) *liveWork {
	t.Helper()
	model := startLiveModel(t)
	h := newRealCreateHarness(t)
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

	app := uuid.NewString()
	ws := h.postWorkspace(t, proto.Workspace{Path: dir, DataDir: t.TempDir(), ClientID: app, YOLO: true})
	t.Cleanup(func() { _ = h.backend.RetireClient(app) })
	require.NoError(t, h.backend.InitAgent(context.Background(), ws.ID, app, true))
	return &liveWork{h: h, model: model, ws: ws, app: app}
}

// chat creates a chat through the route.
func (l *liveWork) chat(t *testing.T, title string) string {
	t.Helper()
	s := liveJSON[proto.Session](t, l.h, http.MethodPost, "/v1/workspaces/"+l.ws.ID+"/sessions",
		proto.Session{Title: title}, http.StatusOK)
	return s.ID
}

// start sends prompt to chat as the app, and waits until its turn holds.
func (l *liveWork) start(t *testing.T, chat, verb, name string) {
	t.Helper()
	require.NoError(t, l.h.backend.SendMessage(l.ws.ID, l.app, proto.AgentMessage{SessionID: chat, Prompt: "live:" + verb + ":" + name}))
	l.model.waitHeld(t, name, 1)
}

// helper starts a turn in chat that asks for a helper, and waits until the
// helper runs.
func (l *liveWork) helper(t *testing.T, chat, name string) agent.Task {
	t.Helper()
	l.start(t, chat, "helper", name)
	l.model.waitHeld(t, name+"-helper", 1)
	var task agent.Task
	require.Eventually(t, func() bool {
		for _, candidate := range agent.SessionTasks(chat) {
			if candidate.Name == name+"-helper" {
				task = candidate
				return true
			}
		}
		return false
	}, 20*time.Second, 20*time.Millisecond)
	return task
}

// job starts a turn in chat that starts a background job, and waits until
// the job is listed.
func (l *liveWork) job(t *testing.T, chat, name string) agent.Process {
	t.Helper()
	l.start(t, chat, "job", name)
	var job agent.Process
	require.Eventually(t, func() bool {
		jobs := agent.SessionBackgroundProcesses(chat)
		if len(jobs) == 1 {
			job = jobs[0]
			return true
		}
		return false
	}, 20*time.Second, 20*time.Millisecond)
	return job
}
