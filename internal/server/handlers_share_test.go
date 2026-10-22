package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/app"
	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/remote"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// shareBase is the URL every request to a test engine goes to; its client
// dials the engine's unix socket whatever the host.
const shareBase = "http://engine"

// shareMarker is an event the tests send to every stream after the events
// under test: once a stream carried it, it carried everything before it.
const shareMarker pubsub.PayloadType = "test_marker"

// shareHarness serves the engine's HTTP API on a unix socket, as the engine
// does, over one workspace.
type shareHarness struct {
	srv     *Server
	backend *backend.Backend
	ws      *backend.Workspace
	agent   *shareFakeAgent
	client  *http.Client
	// logs is everything the server logged: requests, errors and panics.
	logs *shareLog
	// apps are the apps the tests registered; they leave at cleanup.
	apps []string
	// streams is how many event streams the tests opened, one per app.
	streams int
	readers sync.WaitGroup
}

// newShareHarness serves a synthetic workspace on a test app whose agent is
// a fake.
func newShareHarness(t *testing.T) *shareHarness {
	t.Helper()
	appCtx, cancel := context.WithCancel(context.Background())
	a := app.NewForTest(appCtx)
	t.Cleanup(func() {
		cancel()
		a.ShutdownForTest()
	})
	h := &shareHarness{agent: &shareFakeAgent{}}
	a.AgentCoordinator = h.agent
	h.serve(t)
	ws := &backend.Workspace{
		ID:   uuid.NewString(),
		Path: t.TempDir(),
		App:  a,
		Cfg:  config.NewTestStore(&config.Config{}),
	}
	backend.SetWorkspaceShutdownFnForTest(ws, func() {})
	backend.InsertWorkspaceForTest(h.backend, ws)
	h.ws = ws
	return h
}

// serve starts the server on a unix socket in a short temporary folder.
func (h *shareHarness) serve(t *testing.T) {
	t.Helper()
	h.logs = &shareLog{}
	h.srv = &Server{}
	h.srv.backend = backend.New(context.Background(), nil, func() {})
	h.srv.SetLogger(slog.New(slog.NewTextHandler(h.logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	h.srv.installHandler()
	h.backend = h.srv.backend

	// Socket paths are short; a test's own temporary folder can be too
	// long for one.
	dir, err := os.MkdirTemp("", "sk")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "e.sock")
	ln, err := net.Listen("unix", sock) //nolint:noctx
	require.NoError(t, err)
	hs := &http.Server{Handler: h.srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = hs.Serve(ln) }()
	h.client = &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", sock)
		},
	}}
	// LIFO: the apps leave first, so the workspace tears down (its shares
	// stop, its secure entry closes); then the server closes, which ends
	// the streams, and their readers finish.
	t.Cleanup(h.readers.Wait)
	t.Cleanup(func() { _ = hs.Close() })
	t.Cleanup(func() {
		for _, id := range h.apps {
			_ = h.backend.RetireClient(id)
		}
	})
}

// do sends a request as clientID and returns the status and body.
func (h *shareHarness) do(t *testing.T, method, path, clientID string, body any) (int, []byte) {
	t.Helper()
	var reader io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		reader = strings.NewReader(b)
	default:
		data, err := json.Marshal(b)
		require.NoError(t, err)
		reader = bytes.NewReader(data)
	}
	url := shareBase + path
	if clientID != "" {
		url += "?client_id=" + clientID
	}
	req, err := http.NewRequestWithContext(t.Context(), method, url, reader)
	require.NoError(t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := h.client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, out
}

// wsPath is path under the harness workspace.
func (h *shareHarness) wsPath(path string) string {
	return "/v1/workspaces/" + h.ws.ID + path
}

// expectError checks that a request fails with status and code.
func (h *shareHarness) expectError(t *testing.T, method, path, clientID string, body any, status int, code string) proto.Error {
	t.Helper()
	got, out := h.do(t, method, path, clientID, body)
	require.Equal(t, status, got, "%s %s: %s", method, path, out)
	var perr proto.Error
	require.NoError(t, json.Unmarshal(out, &perr))
	require.Equal(t, code, perr.Code, "%s %s", method, path)
	return perr
}

// expectOK checks that a request succeeds and decodes its body into out,
// when given.
func (h *shareHarness) expectOK(t *testing.T, method, path, clientID string, body, out any) {
	t.Helper()
	got, data := h.do(t, method, path, clientID, body)
	require.Equal(t, http.StatusOK, got, "%s %s: %s", method, path, data)
	if out != nil {
		require.NoError(t, json.Unmarshal(data, out))
	}
}

// view reports the chat clientID shows.
func (h *shareHarness) view(t *testing.T, clientID, sessionID string) {
	t.Helper()
	h.expectOK(t, http.MethodPost, h.wsPath("/current-session"), clientID, proto.CurrentSession{SessionID: sessionID}, nil)
}

// mark sends the marker event to every stream of the workspace.
func (h *shareHarness) mark() {
	h.backend.SendAll(h.ws.ID, backend.EngineEvent{Type: shareMarker, Kind: pubsub.UpdatedEvent})
}

// stream opens the event stream of a new app, which registers it, and
// waits until it is attached.
func (h *shareHarness) stream(t *testing.T) (string, *shareStream) {
	t.Helper()
	id := uuid.NewString()
	return id, h.streamAs(t, id)
}

// streamAs opens an event stream of clientID, which has none yet, and waits
// until it is attached.
func (h *shareHarness) streamAs(t *testing.T, clientID string) *shareStream {
	t.Helper()
	if !slices.Contains(h.apps, clientID) {
		h.apps = append(h.apps, clientID)
	}
	h.streams++
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, shareBase+h.wsPath("/events")+"?client_id="+clientID, nil)
	require.NoError(t, err)
	resp, err := h.client.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	s := &shareStream{events: make(chan shareEvent, 1024)}
	h.readers.Go(func() {
		defer resp.Body.Close()
		s.read(resp.Body)
	})
	want := h.streams
	require.Eventually(t, func() bool { return backend.WorkspaceLiveStreamCountForTest(h.ws) >= want },
		5*time.Second, 5*time.Millisecond, "the stream attaches")
	return s
}

// startShare starts the share of sessionID ("" for clientID's new chat)
// from clientID on a fake remote server.
func (h *shareHarness) startShare(t *testing.T, clientID, sessionID string) *shareFakeRemote {
	t.Helper()
	fake := newShareFakeRemote()
	backend.SetShareStarterForTest(h.ws, func(context.Context, *backend.Workspace, string) (backend.ShareServer, error) {
		return newEngineShare(fake), nil
	})
	var status proto.RemoteStatus
	h.expectOK(t, http.MethodPost, h.wsPath("/remote/start"), clientID, proto.RemoteStart{SessionID: sessionID}, &status)
	require.Equal(t, "on", status.State)
	require.Equal(t, sessionID, status.SessionID)
	return fake
}

// shareEvent is one event of an app's stream: its type, kind and payload,
// and the raw line it came in.
type shareEvent struct {
	typ     pubsub.PayloadType
	kind    pubsub.EventType
	payload json.RawMessage
	line    []byte
}

// shareStream is one app's event stream: every byte it carried, and its
// events as they arrive.
type shareStream struct {
	mu     sync.Mutex
	raw    bytes.Buffer
	events chan shareEvent
}

func (s *shareStream) read(body io.Reader) {
	reader := bufio.NewReader(body)
	for {
		line, err := reader.ReadBytes('\n')
		s.mu.Lock()
		s.raw.Write(line)
		s.mu.Unlock()
		if err != nil {
			return
		}
		data, ok := bytes.CutPrefix(bytes.TrimSpace(line), []byte("data:"))
		if !ok {
			continue
		}
		var outer pubsub.Payload
		if json.Unmarshal(bytes.TrimSpace(data), &outer) != nil {
			continue
		}
		var inner struct {
			Type    pubsub.EventType `json:"type"`
			Payload json.RawMessage  `json:"payload"`
		}
		if json.Unmarshal(outer.Payload, &inner) != nil {
			continue
		}
		select {
		case s.events <- shareEvent{typ: outer.Type, kind: inner.Type, payload: inner.Payload, line: line}:
		default:
		}
	}
}

// bytes returns everything the stream carried so far.
func (s *shareStream) bytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return bytes.Clone(s.raw.Bytes())
}

// wait returns the next event of type typ, skipping others.
func (s *shareStream) wait(t *testing.T, typ pubsub.PayloadType) shareEvent {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-s.events:
			if ev.typ == typ {
				return ev
			}
		case <-deadline:
			t.Fatalf("no %s event arrived", typ)
			return shareEvent{}
		}
	}
}

// waitFor decodes the payload of the next event of type typ into out.
func (s *shareStream) waitFor(t *testing.T, typ pubsub.PayloadType, out any) shareEvent {
	t.Helper()
	ev := s.wait(t, typ)
	require.NoError(t, json.Unmarshal(ev.payload, out))
	return ev
}

// quietUntilMarker checks that no event of type typ comes before the marker.
func (s *shareStream) quietUntilMarker(t *testing.T, typ pubsub.PayloadType) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-s.events:
			require.NotEqual(t, typ, ev.typ, "the stream got a %s event", typ)
			if ev.typ == shareMarker {
				return
			}
		case <-deadline:
			t.Fatal("the marker never arrived")
		}
	}
}

// shareLog collects log output.
type shareLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *shareLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *shareLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// shareFakeRemote is a fake remote.Server: the tests hand it phone actions.
type shareFakeRemote struct {
	actions chan *remote.Action
	phones  chan struct{}
	stopped chan struct{}
	once    sync.Once

	mu       sync.Mutex
	status   remote.Status
	presence []remote.Presence
}

func newShareFakeRemote() *shareFakeRemote {
	return &shareFakeRemote{
		actions: make(chan *remote.Action),
		phones:  make(chan struct{}, 1),
		stopped: make(chan struct{}),
		status:  remote.Status{Host: "test-host", Addr: "100.64.0.1:7373"},
	}
}

func (f *shareFakeRemote) Actions() <-chan *remote.Action { return f.actions }
func (f *shareFakeRemote) PhonesChanged() <-chan struct{} { return f.phones }
func (f *shareFakeRemote) Stopped() <-chan struct{}       { return f.stopped }
func (f *shareFakeRemote) Stop()                          { f.once.Do(func() { close(f.stopped) }) }

func (f *shareFakeRemote) SetPresence(p remote.Presence) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.presence = append(f.presence, p)
}

func (f *shareFakeRemote) SetLaunch(string) {}

func (f *shareFakeRemote) Status() remote.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status
}

func (f *shareFakeRemote) presences() []remote.Presence {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]remote.Presence(nil), f.presence...)
}

// offer hands a phone action to the engine, as the share does.
func (f *shareFakeRemote) offer(t *testing.T, a remote.Action) <-chan error {
	t.Helper()
	action, reply := remote.NewTestAction(a)
	select {
	case f.actions <- action:
	case <-time.After(time.Second):
		t.Fatal("the engine did not take the phone action")
	}
	return reply
}

// shareReply waits for a phone action's answer.
func shareReply(t *testing.T, reply <-chan error) error {
	t.Helper()
	select {
	case err := <-reply:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("the phone action got no answer")
		return nil
	}
}

// shareFakeRun is one prompt the fake agent ran.
type shareFakeRun struct {
	sessionID, prompt string
	attachments       []message.Attachment
}

// shareFakeAgent is a fake agent that records the prompts it runs.
type shareFakeAgent struct {
	mu   sync.Mutex
	runs []shareFakeRun
}

func (a *shareFakeAgent) recorded() []shareFakeRun {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]shareFakeRun(nil), a.runs...)
}

func (a *shareFakeAgent) RunAccepted(_ context.Context, _ *agent.AcceptedRun, sessionID, prompt string, attachments ...message.Attachment) (*fantasy.AgentResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.runs = append(a.runs, shareFakeRun{sessionID: sessionID, prompt: prompt, attachments: attachments})
	return nil, nil
}

func (a *shareFakeAgent) Run(context.Context, string, string, ...message.Attachment) (*fantasy.AgentResult, error) {
	return nil, nil
}

func (a *shareFakeAgent) SetMainAgent(string) error                       { return nil }
func (*shareFakeAgent) MainAgentName() string                             { return "coder" }
func (a *shareFakeAgent) BeginAccepted(string) *agent.AcceptedRun         { return nil }
func (a *shareFakeAgent) Cancel(string)                                   {}
func (a *shareFakeAgent) Interrupt(string)                                {}
func (a *shareFakeAgent) CancelAll()                                      {}
func (a *shareFakeAgent) IsSessionBusy(string) bool                       { return false }
func (a *shareFakeAgent) IsBusy() bool                                    { return false }
func (a *shareFakeAgent) QueuedPrompts(string) int                        { return 0 }
func (a *shareFakeAgent) QueuedPromptsList(string) []string               { return nil }
func (a *shareFakeAgent) ClearQueue(string)                               {}
func (a *shareFakeAgent) RecallQueuedPrompt(string) *message.QueuedPrompt { return nil }
func (a *shareFakeAgent) Summarize(context.Context, string) error         { return nil }
func (a *shareFakeAgent) Model() agent.Model                              { return agent.Model{} }
func (a *shareFakeAgent) UpdateModels(context.Context) error              { return nil }
func (a *shareFakeAgent) GenerateTitle(context.Context, string, string)   {}

// TestRemoteRoutes: any app may stop a chat's share and tell what its
// window shows; GET remote lists the shares; only the window an action went
// to may answer it.
func TestRemoteRoutes(t *testing.T) {
	t.Parallel()
	h := newShareHarness(t)
	owner, _ := h.stream(t)
	other, _ := h.stream(t)
	stop := h.wsPath("/remote/stop")
	presence := h.wsPath("/remote/presence")
	done := h.wsPath("/remote/actions/" + uuid.NewString() + "/done")

	h.expectError(t, http.MethodPost, stop, owner, proto.RemoteStop{SessionID: "chat-1"}, http.StatusNotFound, "share_off")
	h.expectError(t, http.MethodPost, done, owner, proto.RemoteActionDone{}, http.StatusNotFound, "share_off")
	h.expectOK(t, http.MethodPost, presence, other, proto.RemotePresence{SessionID: "chat-1", Focused: true}, nil)

	h.view(t, owner, "chat-1")
	fake := h.startShare(t, owner, "chat-1")
	require.Equal(t, []remote.Presence{{SessionID: "chat-1", Focused: true}}, fake.presences(), "the other app's window shows the chat")
	h.expectError(t, http.MethodPost, done, owner, proto.RemoteActionDone{}, http.StatusNotFound, "action_not_found")
	var listed []proto.RemoteStatus
	h.expectOK(t, http.MethodGet, h.wsPath("/remote"), other, nil, &listed)
	require.Equal(t, []proto.RemoteStatus{{State: "on", SessionID: "chat-1", Host: "test-host", Port: 7373}}, listed)

	// The other app turns it off; the owner's window never answered.
	reply := fake.offer(t, remote.Action{Kind: remote.ActYolo, SessionID: "chat-1"})
	h.expectOK(t, http.MethodPost, stop, other, proto.RemoteStop{SessionID: "chat-1"}, nil)
	select {
	case <-fake.Stopped():
	case <-time.After(5 * time.Second):
		t.Fatal("the share did not stop")
	}
	require.EqualError(t, shareReply(t, reply), "Remote Control is off")
	h.expectOK(t, http.MethodGet, h.wsPath("/remote"), owner, nil, &listed)
	require.Empty(t, listed)
}

// TestRemoteActionGoesToTheWindowShowingTheChat: a forwarded yolo reaches
// the stream of the app showing the shared chat as remote_action and no
// other stream; that app's done answers the phone, another's is refused.
func TestRemoteActionGoesToTheWindowShowingTheChat(t *testing.T) {
	t.Parallel()
	h := newShareHarness(t)
	owner, ownerEvents := h.stream(t)
	other, otherEvents := h.stream(t)
	h.view(t, owner, "chat-1")
	fake := h.startShare(t, owner, "chat-1")
	h.view(t, owner, "chat-2")
	h.view(t, other, "chat-1")

	reply := fake.offer(t, remote.Action{Kind: remote.ActYolo, SessionID: "chat-1"})
	var action proto.RemoteAction
	otherEvents.waitFor(t, pubsub.PayloadTypeRemoteAction, &action)
	require.Equal(t, "yolo", action.Kind)
	h.mark()
	ownerEvents.quietUntilMarker(t, pubsub.PayloadTypeRemoteAction)

	path := h.wsPath("/remote/actions/" + action.ActionID + "/done")
	h.expectError(t, http.MethodPost, path, owner, proto.RemoteActionDone{}, http.StatusForbidden, "not_owner")
	h.expectOK(t, http.MethodPost, path, other, proto.RemoteActionDone{}, nil)
	require.NoError(t, shareReply(t, reply))
}

// TestRemoteNewChatSendKeepsUploadsInEngine: a send to the window's new chat
// reaches the window with upload references only; once the window made the
// chat, the engine sends the prompt there with the attachment it kept.
func TestRemoteNewChatSendKeepsUploadsInEngine(t *testing.T) {
	t.Parallel()
	h := newShareHarness(t)
	owner, events := h.stream(t)
	fake := h.startShare(t, owner, "")
	big := bytes.Repeat([]byte("0123456789abcdef"), (5<<20)/16)
	attachment := message.Attachment{FilePath: "/uploads/big.bin", FileName: "big.bin", MimeType: "application/octet-stream", Content: big}

	reply := fake.offer(t, remote.Action{Kind: remote.ActSend, Text: "look at this", Attachments: []message.Attachment{attachment}})
	var action proto.RemoteAction
	ev := events.waitFor(t, pubsub.PayloadTypeRemoteAction, &action)
	require.Less(t, len(ev.line), 4<<10, "the event carries references only")
	require.Equal(t, "new_chat_send", action.Kind)
	require.Empty(t, action.SessionID)
	require.Equal(t, "look at this", action.Text)
	require.Len(t, action.Uploads, 1)
	require.NotEmpty(t, action.Uploads[0].UploadID)
	require.Equal(t, proto.RemoteUpload{UploadID: action.Uploads[0].UploadID, Name: "big.bin", Mime: "application/octet-stream", Size: len(big)}, action.Uploads[0])

	// The window creates its new chat, shows it and reports it: the share
	// moves there.
	h.view(t, owner, "new-chat")
	var listed []proto.RemoteStatus
	h.expectOK(t, http.MethodGet, h.wsPath("/remote"), owner, nil, &listed)
	require.Len(t, listed, 1)
	require.Equal(t, "new-chat", listed[0].SessionID)
	h.expectOK(t, http.MethodPost, h.wsPath("/remote/actions/"+action.ActionID+"/done"), owner, proto.RemoteActionDone{SessionID: "new-chat"}, nil)
	require.NoError(t, shareReply(t, reply))
	require.Eventually(t, func() bool { return len(h.agent.recorded()) == 1 }, 5*time.Second, 10*time.Millisecond)
	run := h.agent.recorded()[0]
	require.Equal(t, "new-chat", run.sessionID)
	require.Equal(t, "look at this", run.prompt)
	require.Len(t, run.attachments, 1)
	require.Equal(t, "big.bin", run.attachments[0].FileName)
	require.True(t, bytes.Equal(big, run.attachments[0].Content), "the kept upload reaches the chat")
	require.Less(t, len(events.bytes()), 64<<10, "no upload bytes on the stream")
}

// TestRemoteStatusEvents: a chat's share, its phone changes and its stop
// reach every app with the chat's ID; a new chat's share is heard of by its
// app only; a stopped share is no longer listed.
func TestRemoteStatusEvents(t *testing.T) {
	t.Parallel()
	h := newShareHarness(t)
	owner, ownerEvents := h.stream(t)
	_, otherEvents := h.stream(t)
	h.view(t, owner, "chat-1")
	fake := h.startShare(t, owner, "chat-1")
	on := proto.RemoteStatus{State: "on", SessionID: "chat-1", Host: "test-host", Port: 7373}
	for _, events := range []*shareStream{ownerEvents, otherEvents} {
		var started proto.RemoteStatus
		events.waitFor(t, pubsub.PayloadTypeRemoteStatus, &started)
		require.Equal(t, on, started)
	}

	fake.mu.Lock()
	fake.status.Phones = []string{"Pixel"}
	fake.mu.Unlock()
	fake.phones <- struct{}{}
	on.Phones = []string{"Pixel"}
	for _, events := range []*shareStream{ownerEvents, otherEvents} {
		var changed proto.RemoteStatus
		events.waitFor(t, pubsub.PayloadTypeRemoteStatus, &changed)
		require.Equal(t, on, changed)
	}

	fake.Stop() // the share stopped on its own
	for _, events := range []*shareStream{ownerEvents, otherEvents} {
		var stopped proto.RemoteStatus
		events.waitFor(t, pubsub.PayloadTypeRemoteStatus, &stopped)
		require.Equal(t, proto.RemoteStatus{State: "off", SessionID: "chat-1"}, stopped)
	}
	var listed []proto.RemoteStatus
	h.expectOK(t, http.MethodGet, h.wsPath("/remote"), owner, nil, &listed)
	require.Empty(t, listed)

	h.view(t, owner, "")
	h.startShare(t, owner, "")
	var started proto.RemoteStatus
	ownerEvents.waitFor(t, pubsub.PayloadTypeRemoteStatus, &started)
	require.Equal(t, proto.RemoteStatus{State: "on", Host: "test-host", Port: 7373}, started)
	h.mark()
	otherEvents.quietUntilMarker(t, pubsub.PayloadTypeRemoteStatus)
}

// TestRemoteShareAdapterKinds: every phone action kind reaches the engine as
// the same kind, and an unknown one keeps its number.
func TestRemoteShareAdapterKinds(t *testing.T) {
	t.Parallel()
	for remoteKind, kind := range map[remote.ActionKind]backend.ShareActionKind{
		remote.ActSend: backend.ShareSend, remote.ActCancel: backend.ShareCancel,
		remote.ActInterrupt: backend.ShareInterrupt, remote.ActBackground: backend.ShareBackground,
		remote.ActClearQueue: backend.ShareClearQueue, remote.ActModel: backend.ShareModel,
		remote.ActEffort: backend.ShareEffort, remote.ActThink: backend.ShareThink,
		remote.ActFast: backend.ShareFast, remote.ActUltracode: backend.ShareUltracode,
		remote.ActYolo: backend.ShareYolo, remote.ActMode: backend.ShareMode,
		remote.ActSummarize: backend.ShareSummarize, remote.ActionKind(99): backend.ShareActionKind(99),
	} {
		require.Equal(t, kind, shareActionFromRemote(&remote.Action{Kind: remoteKind}).Kind)
	}
	require.Equal(t, proto.RemoteSetup{State: "missing", Title: "Install Tailscale", Detail: "d", Action: "install", Button: "Install Tailscale"},
		remoteSetupProto(remote.SetupInfo{State: remote.SetupMissing, Title: "Install Tailscale", Detail: "d", Action: "install", Button: "Install Tailscale"}))
	require.Equal(t, proto.RemoteSetup{State: "unsupported", Title: "t", Detail: "d"},
		remoteSetupProto(remote.SetupInfo{State: remote.SetupUnsupported, Title: "t", Detail: "d", Button: "ignored"}))
}
