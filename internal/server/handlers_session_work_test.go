package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// chatRequest is what a fake model turn is decided on.
type chatRequest struct {
	sessionHash string // x-session-id: session.HashID of the asking chat
	title       bool   // a request for a chat title
	lastRole    string // the role of the conversation's last message
}

// chatAnswer is a fake model turn: text, or a tool call when toolName is
// set. A non-nil wait holds the answer until it is closed.
type chatAnswer struct {
	text               string
	toolName, toolArgs string
	wait               <-chan struct{}
}

// chatModel is an OpenAI-compatible chat server for the provider "fake".
type chatModel struct {
	srv     *httptest.Server
	respond func(chatRequest) chatAnswer
	stop    chan struct{}
	calls   atomic.Int64

	mu   sync.Mutex
	seen []string // one line per request, for failure reports
}

// requests describes the requests the model got.
func (m *chatModel) requests() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return strings.Join(m.seen, "\n")
}

func newChatModel(t *testing.T, respond func(chatRequest) chatAnswer) *chatModel {
	t.Helper()
	m := &chatModel{respond: respond, stop: make(chan struct{})}
	m.srv = httptest.NewServer(http.HandlerFunc(m.serve))
	t.Cleanup(func() {
		close(m.stop)
		m.srv.Close()
	})
	return m
}

func (m *chatModel) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req struct {
		Messages []struct {
			Role string `json:"role"`
		} `json:"messages"`
	}
	_ = json.Unmarshal(body, &req)
	in := chatRequest{
		sessionHash: r.Header.Get("X-Session-Id"),
		title:       bytes.Contains(body, []byte("concise title")),
	}
	if n := len(req.Messages); n > 0 {
		in.lastRole = req.Messages[n-1].Role
	}
	answer := m.respond(in)
	m.mu.Lock()
	m.seen = append(m.seen, fmt.Sprintf("%s %s title=%t messages=%d last=%s -> text=%q tool=%s",
		r.URL.Path, in.sessionHash, in.title, len(req.Messages), in.lastRole, answer.text, answer.toolName))
	m.mu.Unlock()

	w.Header().Set("Content-Type", "text/event-stream")
	if answer.wait != nil {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-answer.wait:
		case <-m.stop:
			return
		case <-r.Context().Done():
			return
		}
	}
	send := func(delta map[string]any, finish any) {
		data, _ := json.Marshal(map[string]any{
			"id": "c1", "object": "chat.completion.chunk", "created": 1, "model": "fake-model",
			"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}},
		})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
	}
	if answer.toolName != "" {
		send(map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{
			"index": 0, "id": fmt.Sprintf("call_%d", m.calls.Add(1)), "type": "function",
			"function": map[string]any{"name": answer.toolName, "arguments": answer.toolArgs},
		}}}, nil)
		send(map[string]any{}, "tool_calls")
	} else {
		send(map[string]any{"role": "assistant", "content": answer.text}, nil)
		send(map[string]any{}, "stop")
	}
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
}

// newModelWorkspace creates a real workspace in project whose configured
// model is m's: its agent is ready, and its helpers' request folders are
// under the test's own temp folder. It returns the workspace and the ID of
// the app holding it.
func newModelWorkspace(t *testing.T, h *e2eHarness, m *chatModel, project string) (*backend.Workspace, string) {
	t.Helper()
	model := map[string]any{"provider": "fake", "model": "fake-model"}
	cfg, err := json.Marshal(map[string]any{
		"options": map[string]any{
			"disable_provider_auto_update": true,
			"request_timeout":              0,
			"check_in_minutes":             0,
		},
		"providers": map[string]any{
			"fake": map[string]any{
				"type": "openai-compat", "base_url": m.srv.URL + "/v1", "api_key": "test",
				"discover_models": false,
				"models": []any{map[string]any{
					"id": "fake-model", "name": "Fake model", "context_window": 128000, "default_max_tokens": 4096,
				}},
			},
		},
		"models": map[string]any{"large": model, "small": model},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(project, "crush.json"), cfg, 0o600))

	h.backend.SetCreateGrace(time.Minute)
	clientID := uuid.NewString()
	created := h.postWorkspace(t, proto.Workspace{Path: project, ClientID: clientID})
	require.True(t, created.Config.IsConfigured(), "the fake model is configured")
	ws, err := h.backend.GetWorkspace(created.ID)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.backend.DeleteWorkspace(created.ID, clientID) })
	return ws, clientID
}

// modelHarness is a real-create harness whose task request folders and
// provider catalog stay local to the test.
func modelHarness(t *testing.T) *e2eHarness {
	t.Helper()
	h := newRealCreateHarness(t)
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("CRUSH_DISABLE_PROVIDER_AUTO_UPDATE", "1")
	return h
}

// spawnTestHelper starts a helper in chat parentID, as `crush spawn` does:
// it writes the request into this process's task request folder and reads
// the answer.
func spawnTestHelper(t *testing.T, parentID string) {
	t.Helper()
	pattern := filepath.Join(os.TempDir(), fmt.Sprintf("crush-%d", os.Getuid()), fmt.Sprintf("tasks-%d-*", os.Getpid()))
	dirs, err := filepath.Glob(pattern)
	require.NoError(t, err)
	require.NotEmpty(t, dirs, "the agent has a task request folder")
	dir := dirs[0]

	data, err := json.Marshal(map[string]any{
		"session": parentID, "cli": "fake", "model": "fake-model", "name": "Fake helper", "prompt": "do the thing",
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "spawn.new"), data, 0o600))
	require.NoError(t, os.Rename(filepath.Join(dir, "spawn.new"), filepath.Join(dir, "spawn.req")))
	var reply struct {
		Task *struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"task"`
		Error string `json:"error"`
	}
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(filepath.Join(dir, "spawn.ack"))
		return err == nil && json.Unmarshal(data, &reply) == nil
	}, 15*time.Second, 20*time.Millisecond, "no answer to the spawn request")
	require.Empty(t, reply.Error)
	require.NotNil(t, reply.Task)
	require.Equal(t, "running", reply.Task.Status)
}

// helperModel answers the parent chat (and titles) at once, and holds each
// helper turn until release is closed; helperRunning gets a value per
// helper turn.
func helperModel(t *testing.T, parentHash *atomic.Value, release <-chan struct{}, helperRunning chan<- struct{}) *chatModel {
	return newChatModel(t, func(r chatRequest) chatAnswer {
		if r.title || r.sessionHash == parentHash.Load() {
			return chatAnswer{text: "parent ok"}
		}
		select {
		case helperRunning <- struct{}{}:
		default:
		}
		return chatAnswer{text: "helper report", wait: release}
	})
}

// handedOver reports whether chat holds a helper's result.
func handedOver(t *testing.T, ws *backend.Workspace, chatID string) bool {
	t.Helper()
	msgs, err := ws.Messages.List(t.Context(), chatID)
	require.NoError(t, err)
	for _, m := range msgs {
		if strings.Contains(m.Content().String(), "crush-task-result") {
			return true
		}
	}
	return false
}

func waitForHelper(t *testing.T, helperRunning <-chan struct{}) {
	t.Helper()
	select {
	case <-helperRunning:
	case <-time.After(30 * time.Second):
		t.Fatal("the helper's turn never reached the model")
	}
}

// TestAgentSessionHasUnfinishedWork: the agent session route reports
// unfinished work while a helper of the chat runs, and none once its
// result has been handed over to the chat.
func TestAgentSessionHasUnfinishedWork(t *testing.T) {
	h := modelHarness(t)
	var parentHash atomic.Value
	parentHash.Store("")
	release := make(chan struct{})
	helperRunning := make(chan struct{}, 4)
	m := helperModel(t, &parentHash, release, helperRunning)
	ws, _ := newModelWorkspace(t, h, m, t.TempDir())
	ctx := t.Context()

	chat, err := ws.Sessions.Create(ctx, "chat")
	require.NoError(t, err)
	parentHash.Store(session.HashID(chat.ID))
	path := "/v1/workspaces/" + ws.ID + "/agent/sessions/" + chat.ID
	require.False(t, callAPI[proto.AgentSession](t, h, http.MethodGet, path, nil).HasUnfinishedWork)

	spawnTestHelper(t, chat.ID)
	waitForHelper(t, helperRunning)
	require.True(t, callAPI[proto.AgentSession](t, h, http.MethodGet, path, nil).HasUnfinishedWork, "a helper runs")

	close(release)
	require.Eventually(t, func() bool {
		return !callAPI[proto.AgentSession](t, h, http.MethodGet, path, nil).HasUnfinishedWork
	}, 30*time.Second, 50*time.Millisecond, "the work is done once the result is handed over")
	require.True(t, handedOver(t, ws, chat.ID), "the chat got the helper's result")
}

// TestSessionListHasUnfinishedWork: the list route fills the flag per
// chat: true for the chat whose helper runs, false for another chat.
func TestSessionListHasUnfinishedWork(t *testing.T) {
	h := modelHarness(t)
	var parentHash atomic.Value
	parentHash.Store("")
	release := make(chan struct{})
	helperRunning := make(chan struct{}, 4)
	m := helperModel(t, &parentHash, release, helperRunning)
	ws, _ := newModelWorkspace(t, h, m, t.TempDir())
	ctx := t.Context()

	chatC, err := ws.Sessions.Create(ctx, "C")
	require.NoError(t, err)
	chatD, err := ws.Sessions.Create(ctx, "D")
	require.NoError(t, err)
	parentHash.Store(session.HashID(chatC.ID))

	spawnTestHelper(t, chatC.ID)
	waitForHelper(t, helperRunning)
	chats := sessionsByID(t, h, ws.ID)
	require.True(t, chats[chatC.ID].HasUnfinishedWork, "C's helper runs")
	require.False(t, chats[chatD.ID].HasUnfinishedWork)

	close(release)
	require.Eventually(t, func() bool {
		return !sessionsByID(t, h, ws.ID)[chatC.ID].HasUnfinishedWork
	}, 30*time.Second, 50*time.Millisecond)
	require.True(t, handedOver(t, ws, chatC.ID))
	require.False(t, sessionsByID(t, h, ws.ID)[chatD.ID].HasUnfinishedWork)
}
