//go:build !windows

package serverctl_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/serverctl"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/workspace"
	"github.com/stretchr/testify/require"
)

// connectionWatcher is the model of a headless program: it passes the
// workspace's connection events and helper (task) events on.
type connectionWatcher struct {
	events chan<- workspace.ConnectionEvent
	tasks  chan<- agent.Task
}

func (m connectionWatcher) Init() tea.Cmd { return nil }

func (m connectionWatcher) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch ev := msg.(type) {
	case workspace.ConnectionEvent:
		select {
		case m.events <- ev:
		default:
		}
	case pubsub.Event[agent.Task]:
		select {
		case m.tasks <- ev.Payload:
		default:
		}
	}
	return m, nil
}

func (m connectionWatcher) View() tea.View { return tea.NewView("") }

// subscribeHeadless runs w's event subscription, as the terminal does,
// with a program that has no input, output, renderer or signal handling,
// and returns the connection and helper events it gets. Cleanup shuts w
// down.
func subscribeHeadless(t *testing.T, w *workspace.ClientWorkspace) (<-chan workspace.ConnectionEvent, <-chan agent.Task) {
	t.Helper()
	events := make(chan workspace.ConnectionEvent, 64)
	tasks := make(chan agent.Task, 64)
	ctx, cancel := context.WithCancel(context.Background())
	prog := tea.NewProgram(connectionWatcher{events: events, tasks: tasks},
		tea.WithContext(ctx), tea.WithInput(nil), tea.WithOutput(io.Discard),
		tea.WithoutRenderer(), tea.WithoutSignalHandler(), tea.WithoutSignals())
	runDone := make(chan struct{})
	go func() {
		_, _ = prog.Run()
		close(runDone)
	}()
	subDone := make(chan struct{})
	go func() {
		w.Subscribe(prog)
		close(subDone)
	}()
	t.Cleanup(func() {
		w.Shutdown()
		select {
		case <-subDone:
		case <-time.After(10 * time.Second):
			t.Error("the subscription did not stop")
		}
		cancel()
		<-runDone
	})
	return events, tasks
}

// waitForConnection waits for a connection event in state.
func waitForConnection(t *testing.T, events <-chan workspace.ConnectionEvent, state workspace.ConnectionState, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-events:
			if ev.State == state {
				return
			}
		case <-deadline:
			t.Fatalf("no connection event %v within %s", state, timeout)
		}
	}
}

// app is a terminal or desktop window on the project's engine: a client
// workspace whose subscription recovers through ensure.
type app struct {
	cw     *workspace.ClientWorkspace
	ws     *proto.Workspace
	events <-chan workspace.ConnectionEvent
	tasks  <-chan agent.Task

	ensures atomic.Int32
	mu      sync.Mutex
	pids    []int // the engines its successful ensures found or started
}

// startApp joins the engine h and subscribes to its events.
func (p *engineProject) startApp(t *testing.T, h serverctl.EngineHandle) *app {
	t.Helper()
	c, ws := p.create(t, h)
	a := &app{ws: ws, cw: workspace.NewClientWorkspace(c, *ws)}
	a.cw.SetEnsureEngine(func(ctx context.Context) error {
		a.ensures.Add(1)
		h, err := p.ensure(ctx)
		if err == nil {
			a.mu.Lock()
			a.pids = append(a.pids, h.PID)
			a.mu.Unlock()
		}
		return err
	})
	a.events, a.tasks = subscribeHeadless(t, a.cw)
	return a
}

func (a *app) enginePIDs() []int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.pids)
}

// streamSettle is how long a test lets new subscriptions open their event
// streams before it kills the engine under them.
const streamSettle = 500 * time.Millisecond

// TestClientRecoversWhenEngineKilled: the engine dies by SIGKILL under a
// subscribed app. The app runs ensure, which starts a new engine; the new
// engine does not know the workspace, so the app creates it again under a
// new ID, and the app is told the connection recovered.
func TestClientRecoversWhenEngineKilled(t *testing.T) {
	p := newEngineProject(t, "")
	h, err := p.ensure(t.Context())
	require.NoError(t, err)
	a := p.startApp(t, h)
	time.Sleep(streamSettle)

	killEngine(t, h.PID)

	waitForConnection(t, a.events, workspace.ConnectionRecovered, 30*time.Second)
	require.GreaterOrEqual(t, a.ensures.Load(), int32(1), "the app ran ensure")
	info, err := p.engineInfo(t)
	require.NoError(t, err)
	require.NotEqual(t, h.PID, info.PID, "a new engine")
	require.Equal(t, []int{info.PID}, a.enginePIDs())
	require.NotEmpty(t, info.WorkspaceID)
	require.NotEqual(t, a.ws.ID, info.WorkspaceID, "the workspace was created again under a new ID")
}

// fakeModel is an OpenAI-compatible chat server for the provider "fake".
// A helper's turn blocks until finishHelpers or the test's end, so the
// helper keeps running; any other turn answers at once.
type fakeModel struct {
	srv        *httptest.Server
	parentHash atomic.Value // string: session.HashID of the parent chat
	release    chan struct{}
	releaseNow sync.Once
	// helperCalls gets the x-session-id of every helper request.
	helperCalls chan string
}

// newFakeModel starts the server. Call it before newEngineProject: its
// cleanup must run after the engines are killed, which unblocks the
// helper's requests.
func newFakeModel(t *testing.T) *fakeModel {
	t.Helper()
	m := &fakeModel{release: make(chan struct{}), helperCalls: make(chan string, 16)}
	m.parentHash.Store("")
	m.srv = httptest.NewServer(http.HandlerFunc(m.serve))
	t.Cleanup(func() {
		m.finishHelpers()
		m.srv.Close()
	})
	return m
}

// finishHelpers lets every waiting and later helper turn answer.
func (m *fakeModel) finishHelpers() {
	m.releaseNow.Do(func() { close(m.release) })
}

func (m *fakeModel) serve(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	w.Header().Set("Content-Type", "text/event-stream")
	text := "parent ok"
	if hash := r.Header.Get("X-Session-Id"); hash != m.parentHash.Load().(string) {
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		select {
		case m.helperCalls <- hash:
		default:
		}
		select {
		case <-m.release:
		case <-r.Context().Done():
			return
		}
		text = "helper report"
	}
	send := func(delta map[string]any, finish any) {
		data, _ := json.Marshal(map[string]any{
			"id": "c1", "object": "chat.completion.chunk", "created": 1, "model": "fake-model",
			"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}},
		})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
	}
	send(map[string]any{"role": "assistant", "content": text}, nil)
	send(map[string]any{}, "stop")
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
}

// config is the global config that makes "fake" the only provider.
func (m *fakeModel) config() map[string]any {
	model := map[string]any{"provider": "fake", "model": "fake-model"}
	return map[string]any{
		"options": map[string]any{
			"disable_default_providers":    true,
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
	}
}

// waitForHelperCall waits for a helper request from the chat with hash.
func (m *fakeModel) waitForHelperCall(t *testing.T, hash string, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case got := <-m.helperCalls:
			if got == hash {
				return
			}
		case <-deadline:
			t.Fatalf("no helper request from chat %s within %s", hash, timeout)
		}
	}
}

// helperTask is the part of a helper's record the tests read.
type helperTask struct {
	ID      string `json:"id"`
	ChildID string `json:"child_id"`
	Status  string `json:"status"`
	// Owner is set in tasks.json: the PID of the engine running it.
	Owner int `json:"owner"`
}

// spawnHelper starts a helper in the engine pid, as `crush spawn` does: it
// writes the request into one of the engine's task request folders and
// reads the answer.
func (p *engineProject) spawnHelper(t *testing.T, pid int, request map[string]any) helperTask {
	t.Helper()
	pattern := filepath.Join(os.TempDir(), fmt.Sprintf("crush-%d", os.Getuid()), fmt.Sprintf("tasks-%d-*", pid))
	var dir string
	require.Eventually(t, func() bool {
		dirs, _ := filepath.Glob(pattern)
		var newest time.Time
		for _, d := range dirs {
			if fi, err := os.Stat(d); err == nil && fi.ModTime().After(newest) {
				dir, newest = d, fi.ModTime()
			}
		}
		return dir != ""
	}, 10*time.Second, 50*time.Millisecond, "the engine has no task request folder")

	data, err := json.Marshal(request)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "spawn.new"), data, 0o600))
	require.NoError(t, os.Rename(filepath.Join(dir, "spawn.new"), filepath.Join(dir, "spawn.req")))

	var reply struct {
		Task  *helperTask `json:"task"`
		Error string      `json:"error"`
	}
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(filepath.Join(dir, "spawn.ack"))
		return err == nil && json.Unmarshal(data, &reply) == nil
	}, 15*time.Second, 50*time.Millisecond, "no answer to the spawn request")
	require.Empty(t, reply.Error)
	require.NotNil(t, reply.Task)
	return *reply.Task
}

// savedHelper reads the helper with childID from the project's tasks.json.
func (p *engineProject) savedHelper(t *testing.T, childID string) (helperTask, bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(p.dataDir, "tasks.json"))
	if err != nil {
		return helperTask{}, false
	}
	var saved []helperTask
	if json.Unmarshal(data, &saved) != nil {
		return helperTask{}, false
	}
	for _, task := range saved {
		if task.ChildID == childID {
			return task, true
		}
	}
	return helperTask{}, false
}

// TestTwoAppsRecoverAndHelperResumes is the plan's crash test: two apps
// on one chat, whose helper is working, when the engine dies by SIGKILL.
// Both apps recover on their own through one new engine, and that engine
// runs the helper again, on its own chat and under the same task ID.
//
// Both apps also get the resumed helper's `helper` event (piece B, 5.3):
// when it finishes, each app's event feed reports it done under the same
// task ID.
func TestTwoAppsRecoverAndHelperResumes(t *testing.T) {
	model := newFakeModel(t)
	p := newEngineProject(t, "")
	writeGlobalConfig(t, p.globalConfig, model.config())
	ctx := t.Context()

	h, err := p.ensure(ctx)
	require.NoError(t, err)
	apps := []*app{p.startApp(t, h), p.startApp(t, h)}
	require.Equal(t, apps[0].ws.ID, apps[1].ws.ID, "both apps share the project's workspace")
	for _, a := range apps {
		require.True(t, a.ws.Config.IsConfigured())
		require.NoError(t, a.cw.InitCoderAgent(ctx))
	}

	chat, err := apps[0].cw.CreateSession(ctx, "chat")
	require.NoError(t, err)
	model.parentHash.Store(session.HashID(chat.ID))
	for _, a := range apps {
		require.NoError(t, a.cw.SetCurrentSession(ctx, chat.ID))
	}

	task := p.spawnHelper(t, h.PID, map[string]any{
		"session": chat.ID, "cli": "fake", "model": "fake-model", "name": "Fake helper", "prompt": "do the thing",
	})
	require.Equal(t, "running", task.Status)
	childHash := session.HashID(task.ChildID)
	model.waitForHelperCall(t, childHash, 30*time.Second)
	time.Sleep(streamSettle)

	killEngine(t, h.PID)

	for _, a := range apps {
		waitForConnection(t, a.events, workspace.ConnectionRecovered, 60*time.Second)
	}
	info, err := p.engineInfo(t)
	require.NoError(t, err)
	require.NotEqual(t, h.PID, info.PID)
	for i, a := range apps {
		pids := a.enginePIDs()
		require.NotEmpty(t, pids, "app %d ran ensure", i)
		for _, pid := range pids {
			require.Equal(t, info.PID, pid, "app %d reached another engine: there must be one", i)
		}
	}

	model.waitForHelperCall(t, childHash, 60*time.Second)
	saved, ok := p.savedHelper(t, task.ChildID)
	require.True(t, ok, "the resumed helper is saved")
	require.Equal(t, task.ID, saved.ID, "the helper keeps its task ID")
	require.Equal(t, info.PID, saved.Owner, "the new engine runs it")

	model.finishHelpers()
	for i, a := range apps {
		waitForHelperDone(t, a.tasks, task.ID, 60*time.Second, i)
	}
}

// waitForHelperDone waits for app i's helper event saying the helper with
// id is done.
func waitForHelperDone(t *testing.T, tasks <-chan agent.Task, id string, timeout time.Duration, i int) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case task := <-tasks:
			if task.ID == id && task.Status == agent.TaskDone {
				return
			}
		case <-deadline:
			t.Fatalf("app %d got no helper event for %s ending done within %s", i, id, timeout)
		}
	}
}
