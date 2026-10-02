package remote

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/stretchr/testify/require"
)

const (
	ownerUser = 7
	phoneAddr = "100.71.87.38"
)

func TestGate(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	g := &gate{user: ownerUser, whois: func(_ context.Context, addr string) (peer, error) {
		calls.Add(1)
		if strings.HasPrefix(addr, phoneAddr+":") {
			return peer{User: ownerUser, Name: "pixel"}, nil
		}
		return peer{User: 8, Name: "someone-else"}, nil
	}}
	ctx := t.Context()

	_, err := g.check(ctx, "192.168.1.20:5000")
	require.ErrorIs(t, err, errNotTailnet)
	_, err = g.check(ctx, "[2001:db8::1]:5000")
	require.ErrorIs(t, err, errNotTailnet)

	p, err := g.check(ctx, phoneAddr+":5000")
	require.NoError(t, err)
	require.Equal(t, "pixel", p.Name)
	_, err = g.check(ctx, phoneAddr+":5001")
	require.NoError(t, err)
	require.Equal(t, int32(1), calls.Load(), "answers are cached per device")

	_, err = g.check(ctx, "100.99.1.2:5000")
	require.Error(t, err)
	_, err = g.check(ctx, "[fd7a:115c:a1e0::9]:5000")
	require.Error(t, err)
}

func TestRefusesAnyoneButTheOwner(t *testing.T) {
	t.Parallel()

	for _, from := range []string{"127.0.0.1", "192.168.1.20", "100.99.1.2"} {
		env := newTestEnv(t, from)
		resp, err := http.Get(env.url + "/v1/share")
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, http.StatusForbidden, resp.StatusCode, from)
	}
}

func TestShareAndLiveUpdates(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, phoneAddr)
	env.src.setChat("s1", "Fix login", userMsg("u1", "fix the login page"))
	env.srv.SetPresence(Presence{SessionID: "s1", Focused: true})

	var share Share
	env.getJSON("/v1/share", &share)
	require.Equal(t, ProtocolVersion, share.Version)
	require.Equal(t, "cachy", share.Host)
	require.NotZero(t, share.Port)

	events := env.stream()
	snap := events.next(t)
	require.Equal(t, "snapshot", string(snap["type"]))
	var items []Item
	require.NoError(t, json.Unmarshal(snap["items"], &items))
	require.Len(t, items, 1)
	require.Equal(t, "fix the login page", items[0].Text)
	var status statusView
	require.NoError(t, json.Unmarshal(snap["status"], &status))
	require.True(t, status.HasChat)
	require.True(t, status.Focused)

	// A new reply streams in.
	reply := assistantMsg("a1", message.TextContent{Text: "On it."})
	reply.SessionID = "s1"
	env.src.publish(pubsub.Event[message.Message]{Type: pubsub.CreatedEvent, Payload: reply})
	ev := events.until(t, "items")
	var upsert []Item
	require.NoError(t, json.Unmarshal(ev["upsert"], &upsert))
	require.Len(t, upsert, 1)
	require.Equal(t, "On it.", upsert[0].Text)

	// Events of other chats are ignored, and a permission prompt shows up.
	other := userMsg("x1", "elsewhere")
	other.SessionID = "s2"
	env.src.publish(pubsub.Event[message.Message]{Type: pubsub.CreatedEvent, Payload: other})
	env.src.setPermission(&permission.PermissionRequest{ID: "p1", SessionID: "s1", ToolName: "bash", Params: map[string]any{"command": "rm -rf build"}})
	ev = events.until(t, "permission")
	var perm permissionView
	require.NoError(t, json.Unmarshal(ev["data"], &perm))
	require.Equal(t, "p1", perm.ID)
	require.Equal(t, "rm -rf build", perm.Command)

	// Following the window to another chat sends a fresh snapshot.
	env.src.setChat("s2", "Other", other)
	env.srv.SetPresence(Presence{SessionID: "s2"})
	snap = events.until(t, "snapshot")
	require.NoError(t, json.Unmarshal(snap["items"], &items))
	require.Len(t, items, 1)
	require.Equal(t, "elsewhere", items[0].Text)
	require.Equal(t, "null", string(snap["permission"]), "the prompt belongs to the other chat")
}

func TestLiteStreamLeavesOutTheChat(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, phoneAddr)
	env.src.setChat("s1", "Chat", userMsg("u1", "hello"))
	env.srv.SetPresence(Presence{SessionID: "s1"})

	events := env.streamPath("/v1/events?lite=1")
	snap := events.next(t)
	require.Equal(t, "snapshot", string(snap["type"]))
	require.Contains(t, snap, "status")
	require.NotContains(t, snap, "items")
	require.NotContains(t, snap, "clis")

	reply := assistantMsg("a1", message.TextContent{Text: "Hi"})
	reply.SessionID = "s1"
	env.src.publish(pubsub.Event[message.Message]{Type: pubsub.CreatedEvent, Payload: reply})
	env.src.setPermission(&permission.PermissionRequest{ID: "p1", SessionID: "s1", ToolName: "bash"})
	for {
		ev := events.next(t)
		require.NotEqual(t, "items", string(ev["type"]))
		if string(ev["type"]) == "permission" {
			break
		}
	}
}

func TestStopTellsThePhones(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, phoneAddr)
	env.src.setChat("s1", "Chat")
	env.srv.SetPresence(Presence{SessionID: "s1"})

	events := env.streamPath("/v1/events?lite=1")
	require.Equal(t, "snapshot", string(events.next(t)["type"]))
	env.srv.Stop()
	events.until(t, "stopped")
}

func TestActionsGoThroughTheTUI(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, phoneAddr)
	env.src.setChat("s1", "Chat")
	env.srv.SetPresence(Presence{SessionID: "s1"})

	go func() {
		for a := range env.srv.Actions() {
			switch {
			case a.Kind == ActSend && a.Text == "hello":
				a.Done(nil)
			case a.Kind == ActModel:
				a.Done(errors.New("Agent is busy, please wait..."))
			default:
				a.Done(errors.New("unexpected action"))
			}
		}
	}()

	code, _ := env.post("/v1/send", map[string]any{"session": "s1", "text": "hello"})
	require.Equal(t, http.StatusOK, code)

	code, body := env.post("/v1/model", map[string]any{"session": "s1", "provider": "codex", "model": "gpt-6-astra"})
	require.Equal(t, http.StatusConflict, code)
	require.Contains(t, body, "Agent is busy")

	// The phone was still showing another chat.
	code, body = env.post("/v1/send", map[string]any{"session": "old", "text": "hello"})
	require.Equal(t, http.StatusConflict, code)
	require.Contains(t, body, "switched")

	code, _ = env.post("/v1/command", map[string]any{"session": "s1", "command": "format-disk"})
	require.Equal(t, http.StatusBadRequest, code)
}

func TestTheNewChatTakesTheFirstMessage(t *testing.T) {
	t.Parallel()

	// The window shows no chat yet: its new chat, before the first message.
	env := newTestEnv(t, phoneAddr)
	got := make(chan *Action, 1)
	go func() {
		for a := range env.srv.Actions() {
			got <- a
			a.Done(nil)
		}
	}()

	code, _ := env.post("/v1/send", map[string]any{"session": "", "text": "hello"})
	require.Equal(t, http.StatusOK, code)
	a := <-got
	require.Equal(t, ActSend, a.Kind)
	require.Empty(t, a.SessionID)

	// It has no name to change until that message starts it.
	code, body := env.post("/v1/rename", map[string]any{"session": "", "title": "Plans"})
	require.Equal(t, http.StatusConflict, code)
	require.Contains(t, body, "first message")
}

func TestPromptsAreAnsweredDirectly(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, phoneAddr)
	env.src.setChat("s1", "Chat")
	env.srv.SetPresence(Presence{SessionID: "s1"})

	env.src.setPermission(&permission.PermissionRequest{ID: "p1", SessionID: "s1", ToolName: "edit"})
	code, _ := env.post("/v1/permission", map[string]any{"session": "s1", "id": "p1", "answer": "session"})
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, []string{"session:p1"}, env.src.granted)
	code, _ = env.post("/v1/permission", map[string]any{"session": "s1", "id": "p1", "answer": "allow"})
	require.Equal(t, http.StatusConflict, code, "already answered")

	env.src.setQuestion(&question.Request{ID: "q1", SessionID: "s1"})
	yes := true
	code, _ = env.post("/v1/question", map[string]any{"session": "s1", "id": "q1", "answers": []question.Answer{{QuestionID: "a", Yes: &yes}}})
	require.Equal(t, http.StatusOK, code)
	require.Len(t, env.src.answers, 1)
	require.True(t, *env.src.answers[0][0].Yes)

	env.src.tasks = []agent.Task{{ID: "t1", SessionID: "s1", Status: agent.TaskRunning}}
	code, _ = env.post("/v1/tasks/t9/stop", map[string]any{"session": "s1"})
	require.Equal(t, http.StatusNotFound, code)
	code, _ = env.post("/v1/tasks/t1/stop", map[string]any{"session": "s1"})
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, []string{"t1"}, env.src.stopped)

	code, _ = env.post("/v1/rename", map[string]any{"session": "s1", "title": "  Better name "})
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "Better name", env.src.sessions["s1"].Title)
}

// testEnv runs a share whose requests all seem to come from one address.
type testEnv struct {
	t   *testing.T
	src *fakeSource
	srv *Server
	url string
}

func newTestEnv(t *testing.T, from string) *testEnv {
	t.Helper()
	src := newFakeSource()
	whois := func(_ context.Context, addr string) (peer, error) {
		if strings.HasPrefix(addr, phoneAddr+":") {
			return peer{User: ownerUser, Name: "pixel"}, nil
		}
		return peer{User: 8}, nil
	}
	srv, err := start(src, self{Host: "cachy", User: ownerUser, Online: true}, whois, func() (net.Listener, error) {
		return net.Listen("tcp", "127.0.0.1:0")
	})
	require.NoError(t, err)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.RemoteAddr = net.JoinHostPort(from, "40000")
		srv.http.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(func() {
		srv.Stop()
		ts.Close()
	})
	return &testEnv{t: t, src: src, srv: srv, url: ts.URL}
}

func (e *testEnv) getJSON(path string, v any) {
	e.t.Helper()
	resp, err := http.Get(e.url + path)
	require.NoError(e.t, err)
	defer resp.Body.Close()
	require.Equal(e.t, http.StatusOK, resp.StatusCode)
	require.NoError(e.t, json.NewDecoder(resp.Body).Decode(v))
}

func (e *testEnv) post(path string, body any) (int, string) {
	e.t.Helper()
	b, err := json.Marshal(body)
	require.NoError(e.t, err)
	resp, err := http.Post(e.url+path, "application/json", strings.NewReader(string(b)))
	require.NoError(e.t, err)
	defer resp.Body.Close()
	var out strings.Builder
	_, _ = bufio.NewReader(resp.Body).WriteTo(&out)
	return resp.StatusCode, out.String()
}

type eventStream struct {
	lines chan string
}

func (e *testEnv) stream() *eventStream {
	e.t.Helper()
	return e.streamPath("/v1/events")
}

func (e *testEnv) streamPath(path string) *eventStream {
	e.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	e.t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.url+path, nil)
	require.NoError(e.t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(e.t, err)
	require.Equal(e.t, http.StatusOK, resp.StatusCode)
	s := &eventStream{lines: make(chan string, 256)}
	go func() {
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				s.lines <- data
			}
		}
		close(s.lines)
	}()
	return s
}

func (s *eventStream) next(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	select {
	case line, ok := <-s.lines:
		require.True(t, ok, "stream closed")
		var ev map[string]json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(line), &ev))
		var typ string
		require.NoError(t, json.Unmarshal(ev["type"], &typ))
		ev["type"] = json.RawMessage(typ)
		return ev
	case <-time.After(5 * time.Second):
		t.Fatal("no event within 5s")
		return nil
	}
}

// until skips events until one of the given type arrives.
func (s *eventStream) until(t *testing.T, typ string) map[string]json.RawMessage {
	t.Helper()
	for {
		if ev := s.next(t); string(ev["type"]) == typ {
			return ev
		}
	}
}

type fakeSource struct {
	events chan pubsub.Event[tea.Msg]

	mu       sync.Mutex
	sessions map[string]session.Session
	msgs     map[string][]message.Message
	perm     *permission.PermissionRequest
	granted  []string
	q        *question.Request
	answers  [][]question.Answer
	tasks    []agent.Task
	stopped  []string
}

func newFakeSource() *fakeSource {
	return &fakeSource{
		events:   make(chan pubsub.Event[tea.Msg], 64),
		sessions: map[string]session.Session{},
		msgs:     map[string][]message.Message{},
	}
}

func (f *fakeSource) setChat(id, title string, msgs ...message.Message) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions[id] = session.Session{ID: id, Title: title}
	for i := range msgs {
		msgs[i].SessionID = id
	}
	f.msgs[id] = msgs
}

func (f *fakeSource) publish(ev any) {
	f.events <- pubsub.Event[tea.Msg]{Type: pubsub.UpdatedEvent, Payload: ev}
}

func (f *fakeSource) setPermission(p *permission.PermissionRequest) {
	f.mu.Lock()
	f.perm = p
	f.mu.Unlock()
	if p != nil {
		f.publish(pubsub.Event[permission.PermissionRequest]{Type: pubsub.CreatedEvent, Payload: *p})
	}
}

func (f *fakeSource) setQuestion(q *question.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.q = q
}

func (f *fakeSource) Events(context.Context) <-chan pubsub.Event[tea.Msg] { return f.events }

func (f *fakeSource) ListMessages(_ context.Context, id string) ([]message.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]message.Message(nil), f.msgs[id]...), nil
}

func (f *fakeSource) GetSession(_ context.Context, id string) (session.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[id]
	if !ok {
		return session.Session{}, errors.New("no such session")
	}
	return s, nil
}

func (f *fakeSource) SaveSession(_ context.Context, s session.Session) (session.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions[s.ID] = s
	return s, nil
}

func (f *fakeSource) AgentIsSessionBusy(string) bool         { return false }
func (f *fakeSource) AgentQueuedPromptsList(string) []string { return nil }

func (f *fakeSource) PendingPermission() (permission.PermissionRequest, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.perm == nil {
		return permission.PermissionRequest{}, false
	}
	return *f.perm, true
}

func (f *fakeSource) resolve(kind string, p permission.PermissionRequest) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.perm == nil || f.perm.ID != p.ID {
		return false
	}
	f.perm = nil
	f.granted = append(f.granted, kind+":"+p.ID)
	return true
}

func (f *fakeSource) PermissionGrant(p permission.PermissionRequest) bool {
	return f.resolve("allow", p)
}

func (f *fakeSource) PermissionGrantPersistent(p permission.PermissionRequest) bool {
	return f.resolve("session", p)
}

func (f *fakeSource) PermissionDeny(p permission.PermissionRequest) bool { return f.resolve("deny", p) }
func (f *fakeSource) PermissionSkipRequests() bool                       { return false }

func (f *fakeSource) PendingQuestion() (question.Request, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.q == nil {
		return question.Request{}, false
	}
	return *f.q, true
}

func (f *fakeSource) QuestionAnswerRequest(id, sessionID string, answers []question.Answer) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.q == nil || f.q.ID != id || f.q.SessionID != sessionID {
		return false
	}
	f.q = nil
	f.answers = append(f.answers, answers)
	return true
}

func (f *fakeSource) QuestionCancelRequest(id, sessionID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.q == nil || f.q.ID != id || f.q.SessionID != sessionID {
		return false
	}
	f.q = nil
	return true
}

func (f *fakeSource) Config() *config.Config { return nil }
func (f *fakeSource) WorkingDir() string     { return "/work" }

func (f *fakeSource) Tasks(string) []agent.Task {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tasks
}

func (f *fakeSource) Processes() []agent.Process { return nil }

func (f *fakeSource) StopTask(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = append(f.stopped, id)
	return nil
}

func (f *fakeSource) KillProcess(int) error                        { return nil }
func (f *fakeSource) Limits(catwalk.Type, string) []cliagent.Limit { return nil }
func (f *fakeSource) CanBackground(catwalk.Type) bool              { return false }
