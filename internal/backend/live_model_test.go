package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/proto/prototest"
	"github.com/stretchr/testify/require"
)

// A fake model for tests that need real agent work (engine-design.md 5.3,
// 5.4 and 5.5): an OpenAI-compatible server whose answer is picked by a
// marker in the latest prompt of each call.
//
//   - live:hold:NAME holds the call open, so its turn runs until it is
//     stopped or the test ends.
//   - live:spawn:NAME runs two bash calls: one starts a background job, one
//     asks the hub for a helper whose prompt is live:hold:NAME-helper. The
//     call after their results is held as NAME.
//   - live:helper:NAME is live:spawn:NAME without the job.
//   - live:start:NAME is live:helper:NAME, except that the helper request
//     waits for the hub's answer, as `crush spawn` does, and the call after
//     it ends the turn, so the helper outlives the turn.
//   - A resumed helper's prompt continues its first one.
//   - Any other prompt (a helper's result, a job notice) and every call
//     without tools (a title) get a short answer, so that turn ends.
var modelMarker = regexp.MustCompile(`live:(hold|spawn|helper|start):([a-z0-9-]+)`)

// modelResumed starts the prompt a resumed helper gets (agent's
// resumePrompt).
const modelResumed = "Crush restarted while you were working"

// fakeModel serves the fake model and counts its calls.
type fakeModel struct {
	srv  *httptest.Server
	done chan struct{}

	mu sync.Mutex
	// held counts the calls of each NAME held open now; calls counts every
	// call of each NAME so far; tools names the tools the latest call of
	// each NAME offered.
	held  map[string]int
	calls map[string]int
	tools map[string][]string
	// notices counts the turns no marker started: helper results and job
	// notices.
	notices int
}

// startFakeModel starts the fake model. Call it before anything that uses
// it, so its cleanup runs last.
func startFakeModel(t *testing.T) *fakeModel {
	t.Helper()
	f := &fakeModel{done: make(chan struct{}), held: map[string]int{}, calls: map[string]int{}, tools: map[string][]string{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(func() {
		close(f.done)
		f.srv.CloseClientConnections()
		f.srv.Close()
	})
	return f
}

// writeProject writes a project whose coder runs on the fake model, and
// returns its folder.
func (f *fakeModel) writeProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cfg := map[string]any{
		"providers": map[string]any{
			"fake": map[string]any{
				"type":     "openai-compat",
				"base_url": f.srv.URL + "/v1",
				"api_key":  "test-key",
				"models": []map[string]any{{
					"id": "fake-model", "name": "Fake model",
					"context_window": 200000, "default_max_tokens": 256,
				}},
			},
		},
		"models": map[string]any{
			"large": map[string]any{"provider": "fake", "model": "fake-model"},
			"small": map[string]any{"provider": "fake", "model": "fake-model"},
		},
		"options": map[string]any{
			"disable_auto_summarize":       true,
			"disable_metrics":              true,
			"disable_provider_auto_update": true,
			"auto_lsp":                     false,
			"notifications":                "disabled",
		},
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "crush.json"), data, 0o600))
	return dir
}

func (f *fakeModel) serve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	last := -1
	for i, m := range req.Messages {
		if m.Role == "user" {
			last = i
		}
	}
	var verb, name string
	afterTools := false
	if last >= 0 {
		text := modelText(req.Messages[last].Content)
		m := modelMarker.FindStringSubmatch(text)
		for i := last - 1; m == nil && i >= 0 && strings.Contains(text, modelResumed); i-- {
			if req.Messages[i].Role == "user" {
				m = modelMarker.FindStringSubmatch(modelText(req.Messages[i].Content))
			}
		}
		if m != nil {
			verb, name = m[1], m[2]
		}
		for _, m := range req.Messages[last+1:] {
			afterTools = afterTools || m.Role == "tool"
		}
	}
	if name != "" && len(req.Tools) > 0 {
		names := make([]string, 0, len(req.Tools))
		for _, tool := range req.Tools {
			names = append(names, tool.Function.Name)
		}
		f.mu.Lock()
		f.tools[name] = names
		f.mu.Unlock()
	}
	w.Header().Set("Content-Type", "text/event-stream")
	switch {
	case len(req.Tools) == 0:
		writeModelText(w, "Live test")
	case verb == "":
		f.mu.Lock()
		f.notices++
		f.mu.Unlock()
		writeModelText(w, "Noted.")
	case verb != "hold" && !afterTools:
		f.mu.Lock()
		f.calls[name]++
		f.mu.Unlock()
		writeModelToolCalls(w, name, verb)
	case verb == "start":
		writeModelText(w, "Started.")
	default:
		f.hold(w, r, name)
	}
}

// hold keeps a call open until it is cancelled or the test ends.
func (f *fakeModel) hold(w http.ResponseWriter, r *http.Request, name string) {
	f.mu.Lock()
	f.calls[name]++
	f.held[name]++
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.held[name]--
		f.mu.Unlock()
	}()
	w.WriteHeader(http.StatusOK)
	writeModelChunk(w, map[string]any{"role": "assistant", "content": "Working"}, nil)
	if fl, ok := w.(http.Flusher); ok {
		fl.Flush()
	}
	select {
	case <-r.Context().Done():
	case <-f.done:
	}
}

// heldNow is how many calls of name are held open now.
func (f *fakeModel) heldNow(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.held[name]
}

// callsOf is how many calls of name came so far.
func (f *fakeModel) callsOf(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[name]
}

// toolsOf names the tools the latest call of name offered.
func (f *fakeModel) toolsOf(name string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.tools[name])
}

// noticeCount is how many turns no marker started came so far.
func (f *fakeModel) noticeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.notices
}

// waitHeld waits until n calls of name are held open.
func (f *fakeModel) waitHeld(t *testing.T, name string, n int) {
	t.Helper()
	require.Eventually(t, func() bool { return f.heldNow(name) == n }, 20*time.Second, 10*time.Millisecond,
		"model calls held for %s: have %d, want %d", name, f.heldNow(name), n)
}

// modelText is a message's text, whether its content is a string or parts.
func modelText(raw json.RawMessage) string {
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

func writeModelChunk(w http.ResponseWriter, delta map[string]any, finish any) {
	data, _ := json.Marshal(map[string]any{
		"id": "live", "object": "chat.completion.chunk", "created": 1, "model": "fake-model",
		"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}},
	})
	fmt.Fprintf(w, "data: %s\n\n", data)
}

func writeModelEnd(w http.ResponseWriter, finish string) {
	writeModelChunk(w, map[string]any{}, finish)
	data, _ := json.Marshal(map[string]any{
		"id": "live", "object": "chat.completion.chunk", "created": 1, "model": "fake-model",
		"choices": []any{}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 2, "total_tokens": 12},
	})
	fmt.Fprintf(w, "data: %s\n\n", data)
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func writeModelText(w http.ResponseWriter, text string) {
	writeModelChunk(w, map[string]any{"role": "assistant", "content": text}, nil)
	writeModelEnd(w, "stop")
}

// writeModelToolCalls answers live:spawn:NAME, live:helper:NAME and
// live:start:NAME: a helper request and, for spawn, a background job, all
// through bash.
func writeModelToolCalls(w http.ResponseWriter, name, verb string) {
	type toolCall struct {
		id   string
		args []byte
	}
	var calls []toolCall
	if verb == "spawn" {
		args, _ := json.Marshal(map[string]any{
			"description":       "Start the " + name + " job",
			"command":           "sleep 120",
			"run_in_background": true,
		})
		calls = append(calls, toolCall{"call_job_" + name, args})
	}
	request := fmt.Sprintf(`{"session":"%%s","cli":"fake","prompt":"live:hold:%s-helper","name":"%s-helper"}`, name, name)
	command := fmt.Sprintf(`printf '%s' "$CRUSH_SESSION_ID" > "$CRUSH_TASKS_DIR/%s.tmp" && mv "$CRUSH_TASKS_DIR/%s.tmp" "$CRUSH_TASKS_DIR/%s.req"`,
		request, name, name, name)
	if verb == "start" {
		command += fmt.Sprintf(` && until [ -e "$CRUSH_TASKS_DIR/%s.ack" ]; do sleep 0.1; done`, name)
	}
	helper, _ := json.Marshal(map[string]any{
		"description": "Ask for the " + name + " helper",
		"command":     command,
	})
	calls = append(calls, toolCall{"call_helper_" + name, helper})
	for i, call := range calls {
		delta := map[string]any{"tool_calls": []any{map[string]any{
			"index": i, "id": call.id, "type": "function",
			"function": map[string]any{"name": "bash", "arguments": string(call.args)},
		}}}
		if i == 0 {
			delta["role"] = "assistant"
		}
		writeModelChunk(w, delta, nil)
	}
	writeModelEnd(w, "tool_calls")
}

// liveRig is a backend whose workspaces run their coder on the fake model.
type liveRig struct {
	b     *Backend
	model *fakeModel
}

func newLiveRig(t *testing.T) *liveRig {
	t.Helper()
	xdgIsolated(t)
	t.Setenv("CRUSH_DISABLE_PROVIDER_AUTO_UPDATE", "1")
	model := startFakeModel(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	b := New(ctx, nil, func() {})
	b.SetCreateGrace(time.Minute)
	b.SetIdleShutdownDelay(time.Minute)
	t.Cleanup(func() { closeLiveRig(b) })
	return &liveRig{b: b, model: model}
}

// closeLiveRig retires every app, so every workspace tears down.
func closeLiveRig(b *Backend) {
	for _, ws := range b.workspaces.Seq2() {
		ws.clientsMu.Lock()
		ids := make([]string, 0, len(ws.clients))
		for id := range ws.clients {
			ids = append(ids, id)
		}
		ws.clientsMu.Unlock()
		for _, id := range ids {
			_ = b.RetireClient(id)
		}
	}
}

// project is a project folder and its data folder.
type liveProject struct {
	dir, dataDir string
}

func (r *liveRig) project(t *testing.T) liveProject {
	t.Helper()
	return liveProject{dir: r.model.writeProject(t), dataDir: t.TempDir()}
}

// open opens p for each app the way the desktop does: create (or join),
// attach a stream, start the agent.
func (r *liveRig) open(t *testing.T, p liveProject, apps ...string) *Workspace {
	t.Helper()
	var ws *Workspace
	for _, app := range apps {
		w, _, err := r.b.CreateWorkspace(prototest.CreateRequest(proto.Workspace{
			Path: p.dir, DataDir: p.dataDir, ClientID: app, YOLO: true,
		}))
		require.NoError(t, err)
		ws = w
		require.NoError(t, r.b.AttachClient(ws.ID, app))
		require.NoError(t, r.b.InitAgent(context.Background(), ws.ID, app, true))
	}
	require.NotNil(t, ws.coordinator(), "the project's coder must run on the fake model")
	return ws
}

// chat creates a chat.
func (r *liveRig) chat(t *testing.T, ws *Workspace, title string) string {
	t.Helper()
	s, err := r.b.CreateSession(context.Background(), ws.ID, title)
	require.NoError(t, err)
	return s.ID
}

// send sends prompt to chat as app, with submission ID id ("" for none).
func (r *liveRig) send(t *testing.T, ws *Workspace, app, chat, id, prompt string) {
	t.Helper()
	require.NoError(t, r.b.SendMessage(ws.ID, app, proto.AgentMessage{SessionID: chat, SubmissionID: id, Prompt: prompt}))
}

// hold sends live:hold:NAME as app and waits until its turn runs.
func (r *liveRig) hold(t *testing.T, ws *Workspace, app, chat, name string) {
	t.Helper()
	r.send(t, ws, app, chat, name, "live:hold:"+name)
	r.model.waitHeld(t, name, 1)
}

// spawnWork has app start a turn in chat that starts a background job and
// a helper, and waits until the turn, the helper and the job all run.
func (r *liveRig) spawnWork(t *testing.T, ws *Workspace, app, chat, name string) (agent.Task, agent.Process) {
	t.Helper()
	r.send(t, ws, app, chat, name, "live:spawn:"+name)
	r.model.waitHeld(t, name, 1)
	r.model.waitHeld(t, name+"-helper", 1)
	var task agent.Task
	var job agent.Process
	require.Eventually(t, func() bool {
		var okTask, okJob bool
		task, okTask = sessionTask(chat, name+"-helper")
		jobs := agent.SessionBackgroundProcesses(chat)
		if len(jobs) == 1 {
			job, okJob = jobs[0], true
		}
		return okTask && okJob
	}, 20*time.Second, 20*time.Millisecond, "the helper and the job of %s must run", name)
	return task, job
}

// spawnHelper has app start a turn in chat that starts a helper, and waits
// until the turn and the helper run.
func (r *liveRig) spawnHelper(t *testing.T, ws *Workspace, app, chat, name string) agent.Task {
	t.Helper()
	r.send(t, ws, app, chat, name, "live:helper:"+name)
	r.model.waitHeld(t, name, 1)
	r.model.waitHeld(t, name+"-helper", 1)
	var task agent.Task
	require.Eventually(t, func() bool {
		var ok bool
		task, ok = sessionTask(chat, name+"-helper")
		return ok
	}, 20*time.Second, 20*time.Millisecond, "the helper of %s must run", name)
	return task
}

// sessionTask finds a chat's helper by name.
func sessionTask(chat, name string) (agent.Task, bool) {
	for _, t := range agent.SessionTasks(chat) {
		if t.Name == name {
			return t, true
		}
	}
	return agent.Task{}, false
}
