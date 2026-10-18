package backend

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/app"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// shareTestServer is a fake phone share. Like the real one, it hands over
// each action only when the engine takes it.
type shareTestServer struct {
	actions  chan *ShareAction
	phones   chan struct{}
	stopped  chan struct{}
	stopOnce sync.Once
	stops    atomic.Int32

	mu       sync.Mutex
	status   ShareStatus
	presence []proto.RemotePresence
}

func newShareTestServer() *shareTestServer {
	return &shareTestServer{
		actions: make(chan *ShareAction),
		phones:  make(chan struct{}, 1),
		stopped: make(chan struct{}),
		status:  ShareStatus{Host: "test-host", Port: 7373},
	}
}

func (s *shareTestServer) Actions() <-chan *ShareAction   { return s.actions }
func (s *shareTestServer) PhonesChanged() <-chan struct{} { return s.phones }
func (s *shareTestServer) Stopped() <-chan struct{}       { return s.stopped }

func (s *shareTestServer) SetPresence(p proto.RemotePresence) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.presence = append(s.presence, p)
}

func (s *shareTestServer) Status() ShareStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *shareTestServer) Stop() {
	s.stops.Add(1)
	s.stopOnce.Do(func() { close(s.stopped) })
}

// offer hands a to the engine, as a phone request does, and fails the test
// when the engine does not take it within 100 ms.
func (s *shareTestServer) offer(t *testing.T, a *ShareAction) {
	t.Helper()
	select {
	case s.actions <- a:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("the engine did not take the phone action within 100 ms")
	}
}

// shareTestAction builds a phone action whose answer arrives on the
// returned channel. A second answer fails the test.
func shareTestAction(t *testing.T, a ShareAction) (*ShareAction, <-chan error) {
	reply := make(chan error, 1)
	a.Done = func(err error) {
		select {
		case reply <- err:
		default:
			t.Errorf("phone action %d answered twice", a.Kind)
		}
	}
	return &a, reply
}

// shareTestRecv receives from ch, failing the test after d.
func shareTestRecv[T any](t *testing.T, ch <-chan T, d time.Duration) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(d):
		t.Fatal("nothing arrived in time")
		var zero T
		return zero
	}
}

// shareTestReply waits for a phone action's answer.
func shareTestReply(t *testing.T, reply <-chan error) error {
	t.Helper()
	return shareTestRecv(t, reply, 5*time.Second)
}

// shareTestRun is one prompt the fake agent ran.
type shareTestRun struct {
	sessionID, prompt, submissionID string
	attachments                     []message.Attachment
}

// shareTestCoordinator is a fake agent that records what the engine asks of
// it. After holdInterrupts, every Interrupt waits until releaseInterrupts.
type shareTestCoordinator struct {
	mu         sync.Mutex
	busy       bool
	runs       []shareTestRun
	interrupts []string
	clears     []string
	updates    int
	summaries  chan string

	gate     chan struct{}
	gateOnce sync.Once
}

func newShareTestCoordinator() *shareTestCoordinator {
	return &shareTestCoordinator{summaries: make(chan string, 4)}
}

func (c *shareTestCoordinator) holdInterrupts() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gate = make(chan struct{})
}

func (c *shareTestCoordinator) releaseInterrupts() {
	c.mu.Lock()
	gate := c.gate
	c.mu.Unlock()
	if gate != nil {
		c.gateOnce.Do(func() { close(gate) })
	}
}

func (c *shareTestCoordinator) setBusy(busy bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.busy = busy
}

func (c *shareTestCoordinator) recorded() (runs []shareTestRun, interrupts, clears []string, updates int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]shareTestRun(nil), c.runs...), append([]string(nil), c.interrupts...), append([]string(nil), c.clears...), c.updates
}

func (c *shareTestCoordinator) RunAccepted(ctx context.Context, _ *agent.AcceptedRun, sessionID, prompt string, attachments ...message.Attachment) (*fantasy.AgentResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.runs = append(c.runs, shareTestRun{sessionID: sessionID, prompt: prompt, submissionID: message.SubmissionID(ctx), attachments: attachments})
	return nil, nil
}

func (c *shareTestCoordinator) Interrupt(sessionID string) {
	c.mu.Lock()
	gate := c.gate
	c.mu.Unlock()
	if gate != nil {
		<-gate
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.interrupts = append(c.interrupts, sessionID)
}

func (c *shareTestCoordinator) ClearQueue(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clears = append(c.clears, sessionID)
}

func (c *shareTestCoordinator) Summarize(_ context.Context, sessionID string) error {
	c.summaries <- sessionID
	return nil
}

func (c *shareTestCoordinator) UpdateModels(context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.updates++
	return nil
}

func (c *shareTestCoordinator) IsBusy() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.busy
}

func (c *shareTestCoordinator) Run(context.Context, string, string, ...message.Attachment) (*fantasy.AgentResult, error) {
	return nil, nil
}

func (c *shareTestCoordinator) SetMainAgent(string) error                       { return nil }
func (c *shareTestCoordinator) BeginAccepted(string) *agent.AcceptedRun         { return nil }
func (c *shareTestCoordinator) Cancel(string)                                   {}
func (c *shareTestCoordinator) CancelAll()                                      {}
func (c *shareTestCoordinator) IsSessionBusy(string) bool                       { return false }
func (c *shareTestCoordinator) QueuedPrompts(string) int                        { return 0 }
func (c *shareTestCoordinator) QueuedPromptsList(string) []string               { return nil }
func (c *shareTestCoordinator) RecallQueuedPrompt(string) *message.QueuedPrompt { return nil }
func (c *shareTestCoordinator) Model() agent.Model                              { return agent.Model{} }
func (c *shareTestCoordinator) GenerateTitle(context.Context, string, string)   {}

// shareTestWorkspace returns a backend with one synthetic workspace on a
// test app whose agent is coord, or none when coord is nil. Cleanup stops
// its shares and closes its secure entry.
func shareTestWorkspace(t *testing.T, coord *shareTestCoordinator) (*Backend, *Workspace) {
	t.Helper()
	ctx := context.Background()
	a := app.NewForTest(ctx)
	t.Cleanup(a.ShutdownForTest)
	if coord != nil {
		a.AgentCoordinator = coord
	}
	b := New(ctx, nil, func() {})
	ws := &Workspace{ID: uuid.NewString(), Path: t.TempDir(), App: a, shutdownFn: func() {}}
	InsertWorkspaceForTest(b, ws)
	t.Cleanup(func() { b.shareTeardown(ws) })
	return b, ws
}

// shareTestRegister registers a new app on ws, attached but with no
// engine-event stream.
func shareTestRegister(t *testing.T, b *Backend, ws *Workspace) string {
	t.Helper()
	id := uuid.NewString()
	require.NoError(t, RegisterClientForTesting(b, ws, id))
	require.NoError(t, b.AttachClient(ws.ID, id))
	return id
}

// shareTestSubscribe opens an engine-event stream for clientID.
func shareTestSubscribe(t *testing.T, b *Backend, ws *Workspace, clientID string) <-chan any {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	events, err := b.SubscribeClient(ctx, ws.ID, clientID)
	require.NoError(t, err)
	return events
}

// shareTestApp registers a new app on ws with one live stream, and returns
// its client ID and that stream's engine events.
func shareTestApp(t *testing.T, b *Backend, ws *Workspace) (string, <-chan any) {
	t.Helper()
	id := shareTestRegister(t, b, ws)
	return id, shareTestSubscribe(t, b, ws, id)
}

// shareTestView reports that clientID shows sessionID.
func shareTestView(t *testing.T, b *Backend, ws *Workspace, clientID, sessionID string) {
	t.Helper()
	require.NoError(t, b.SetCurrentSession(t.Context(), ws.ID, clientID, sessionID))
}

// shareTestStart starts owner's share on a fake server and returns it.
func shareTestStart(t *testing.T, b *Backend, ws *Workspace, owner string) *shareTestServer {
	t.Helper()
	srv := newShareTestServer()
	SetShareStarterForTest(ws, func(context.Context, *Workspace, string) (ShareServer, error) { return srv, nil })
	status, err := b.StartRemote(t.Context(), ws.ID, owner, "")
	require.NoError(t, err)
	require.Equal(t, remoteStateOn, status.State)
	return srv
}

// shareTestFill queues n filler events on clientID's streams.
func shareTestFill(b *Backend, ws *Workspace, clientID string, n int) {
	for range n {
		b.SendTo(ws.ID, clientID, EngineEvent{Type: "filler", Kind: pubsub.UpdatedEvent})
	}
}

// shareTestDrain returns the engine events queued on a stream now.
func shareTestDrain(events <-chan any) []EngineEvent {
	var out []EngineEvent
	for {
		select {
		case ev := <-events:
			if e, ok := ev.(EngineEvent); ok {
				out = append(out, e)
			}
		default:
			return out
		}
	}
}

// shareTestWaitEvent waits for the next engine event of type typ on a
// stream, skipping others.
func shareTestWaitEvent(t *testing.T, events <-chan any, typ pubsub.PayloadType) EngineEvent {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-events:
			if e, ok := ev.(EngineEvent); ok && e.Type == typ {
				return e
			}
		case <-deadline:
			t.Fatalf("no %s event arrived", typ)
			return EngineEvent{}
		}
	}
}

// shareTestConfig loads a real configuration from a private home folder,
// with one provider whose model has reasoning levels, so preference changes
// are written to the test's own file. It sets environment variables, so its
// test cannot run in parallel.
func shareTestConfig(t *testing.T) *config.ConfigStore {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("CRUSH_GLOBAL_CONFIG", filepath.Join(home, "config", "crush"))
	t.Setenv("CRUSH_GLOBAL_DATA", filepath.Join(home, "data", "crush"))
	t.Setenv("CRUSH_DISABLE_PROVIDER_AUTO_UPDATE", "1")
	dir := filepath.Join(home, "config", "crush")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "crush.json"), []byte(`{
  "providers": {
    "phone-test": {
      "name": "Phone test",
      "type": "openai-compat",
      "base_url": "http://127.0.0.1:9/v1",
      "api_key": "test-key",
      "models": [{
        "id": "thinker", "name": "Thinker", "context_window": 100000, "default_max_tokens": 1000,
        "can_reason": true, "reasoning_levels": ["low", "high"], "default_reasoning_effort": "low"
      }]
    }
  },
  "models": {
    "large": {"provider": "phone-test", "model": "thinker"},
    "small": {"provider": "phone-test", "model": "thinker"}
  }
}`), 0o600))
	store, err := config.Load(t.TempDir(), t.TempDir(), false)
	require.NoError(t, err)
	_, ok := store.Config().Agents[config.AgentCoder]
	require.True(t, ok, "the test provider sets up the coder")
	return store
}

// TestRemoteStartOneSharePerApp: a second start from the same app returns
// its running share; another app gets its own.
func TestRemoteStartOneSharePerApp(t *testing.T) {
	t.Parallel()
	b, ws := shareTestWorkspace(t, nil)
	app1, _ := shareTestApp(t, b, ws)
	app2, _ := shareTestApp(t, b, ws)

	var mu sync.Mutex
	var launches []string
	SetShareStarterForTest(ws, func(_ context.Context, _ *Workspace, launchID string) (ShareServer, error) {
		mu.Lock()
		defer mu.Unlock()
		launches = append(launches, launchID)
		srv := newShareTestServer()
		srv.status.Port = 7000 + len(launches)
		return srv, nil
	})

	first, err := b.StartRemote(t.Context(), ws.ID, app1, "launch-1")
	require.NoError(t, err)
	require.Equal(t, proto.RemoteStatus{State: "on", Host: "test-host", Port: 7001}, first)

	// Starts that race each other get the same share too.
	results := make(chan proto.RemoteStatus, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			again, err := b.StartRemote(context.Background(), ws.ID, app1, "launch-1")
			if err != nil {
				again.Error = err.Error()
			}
			results <- again
		})
	}
	wg.Wait()
	close(results)
	for again := range results {
		require.Equal(t, first, again)
	}

	other, err := b.StartRemote(t.Context(), ws.ID, app2, "")
	require.NoError(t, err)
	require.Equal(t, 7002, other.Port)
	mu.Lock()
	require.Equal(t, []string{"launch-1", ""}, launches, "one share per app, each with its own launch ID")
	mu.Unlock()

	got, err := b.RemoteStatus(ws.ID, app1)
	require.NoError(t, err)
	require.Equal(t, first, got)
}

// TestRemoteEngineRunsActions: send, cancel, interrupt, clear_queue,
// summarize and effort run in the engine and reply, with no remote_action
// event; the send is the owner's prompt.
func TestRemoteEngineRunsActions(t *testing.T) {
	store := shareTestConfig(t)
	coord := newShareTestCoordinator()
	b, ws := shareTestWorkspace(t, coord)
	ws.Cfg = store
	owner, events := shareTestApp(t, b, ws)
	shareTestView(t, b, ws, owner, "chat-1")

	type sent struct {
		clientID string
		msg      proto.AgentMessage
	}
	sends := make(chan sent, 4)
	ws.share.send = func(workspaceID, clientID string, msg proto.AgentMessage) error {
		sends <- sent{clientID: clientID, msg: msg}
		return b.SendMessage(workspaceID, clientID, msg)
	}
	srv := shareTestStart(t, b, ws, owner)
	run := func(a ShareAction) {
		t.Helper()
		action, reply := shareTestAction(t, a)
		srv.offer(t, action)
		require.NoError(t, shareTestReply(t, reply), "phone action %d", a.Kind)
	}

	attachment := message.Attachment{FileName: "note.txt", MimeType: "text/plain", Content: []byte("from the phone")}
	run(ShareAction{Kind: ShareSend, SessionID: "chat-1", Text: "hello", Attachments: []message.Attachment{attachment}})
	s := shareTestRecv(t, sends, 5*time.Second)
	require.Equal(t, owner, s.clientID, "the send counts as the owner's prompt")
	_, err := uuid.Parse(s.msg.SubmissionID)
	require.NoError(t, err, "the send has a new submission ID")
	require.Equal(t, "chat-1", s.msg.SessionID)
	require.Equal(t, "hello", s.msg.Prompt)
	require.Eventually(t, func() bool {
		runs, _, _, _ := coord.recorded()
		return len(runs) == 1
	}, 5*time.Second, 10*time.Millisecond)
	runs, _, _, _ := coord.recorded()
	require.Equal(t, shareTestRun{
		sessionID:    "chat-1",
		prompt:       "hello",
		submissionID: s.msg.SubmissionID,
		attachments:  []message.Attachment{attachment},
	}, runs[0])

	run(ShareAction{Kind: ShareCancel, SessionID: "chat-1"})
	run(ShareAction{Kind: ShareInterrupt, SessionID: "chat-1"})
	run(ShareAction{Kind: ShareClearQueue, SessionID: "chat-1"})
	_, interrupts, clears, _ := coord.recorded()
	require.Equal(t, []string{"chat-1", "chat-1"}, interrupts, "cancel is interrupt, as in the terminal")
	require.Equal(t, []string{"chat-1"}, clears)

	run(ShareAction{Kind: ShareSummarize, SessionID: "chat-1"})
	require.Equal(t, "chat-1", shareTestRecv(t, coord.summaries, 5*time.Second))

	run(ShareAction{Kind: ShareEffort, SessionID: "chat-1", Effort: "high"})
	require.Equal(t, "high", store.Config().Models[config.SelectedModelTypeLarge].ReasoningEffort)
	_, _, _, updates := coord.recorded()
	require.Positive(t, updates, "the agent's models are rebuilt")

	for _, ev := range shareTestDrain(events) {
		require.NotEqual(t, pubsub.PayloadTypeRemoteAction, ev.Type, "an engine action reached the window")
	}
}

// TestRemoteEngineActionTexts: the engine answers with the terminal's exact
// texts.
func TestRemoteEngineActionTexts(t *testing.T) {
	t.Parallel()
	coord := newShareTestCoordinator()
	b, ws := shareTestWorkspace(t, coord)
	ws.Cfg = config.NewTestStore(&config.Config{
		Models: map[config.SelectedModelType]config.SelectedModel{
			config.SelectedModelTypeLarge: {Provider: "plain", Model: "m"},
		},
		Providers: csync.NewMapFrom(map[string]config.ProviderConfig{
			"plain": {ID: "plain", Type: catwalk.TypeOpenAICompat, Models: []catwalk.Model{{ID: "m"}}},
		}),
		Agents: map[string]config.Agent{config.AgentCoder: {ID: config.AgentCoder, Model: config.SelectedModelTypeLarge}},
	})
	owner, _ := shareTestApp(t, b, ws)
	shareTestView(t, b, ws, owner, "chat-1")
	srv := shareTestStart(t, b, ws, owner)
	answer := func(a ShareAction) error {
		t.Helper()
		action, reply := shareTestAction(t, a)
		srv.offer(t, action)
		return shareTestReply(t, reply)
	}

	require.EqualError(t, answer(ShareAction{Kind: ShareSend, SessionID: "chat-2", Text: "x"}), "the Crush window switched to another chat")

	coord.setBusy(true)
	for _, kind := range []ShareActionKind{ShareModel, ShareEffort, ShareThink, ShareFast, ShareUltracode, ShareSummarize} {
		require.EqualError(t, answer(ShareAction{Kind: kind, SessionID: "chat-1"}), "Agent is busy, please wait...", "kind %d", kind)
	}
	coord.setBusy(false)

	require.EqualError(t, answer(ShareAction{Kind: ShareFast, SessionID: "chat-1"}), "FAST mode requires a Codex model")
	require.EqualError(t, answer(ShareAction{Kind: ShareActionKind(42), SessionID: "chat-1"}), "unknown request 42")
	require.EqualError(t, answer(ShareAction{Kind: ShareModel, SessionID: "chat-1", Provider: "nope", Model: "m"}), "that CLI is not set up on the computer")
	require.EqualError(t, answer(ShareAction{Kind: ShareModel, SessionID: "chat-1", Provider: "plain", Model: "nope"}), "that model is not available on the computer")
	require.EqualError(t, answer(ShareAction{Kind: ShareBackground, SessionID: "chat-1"}), "no running command can move to the background")

	shareTestView(t, b, ws, owner, "")
	require.EqualError(t, answer(ShareAction{Kind: ShareSummarize}), "a new chat has nothing to summarize yet")

	// Before the agent started, a send has no agent to go to.
	b2, ws2 := shareTestWorkspace(t, nil)
	owner2, _ := shareTestApp(t, b2, ws2)
	shareTestView(t, b2, ws2, owner2, "chat-1")
	srv2 := shareTestStart(t, b2, ws2, owner2)
	action, reply := shareTestAction(t, ShareAction{Kind: ShareSend, SessionID: "chat-1", Text: "x"})
	srv2.offer(t, action)
	require.EqualError(t, shareTestReply(t, reply), "the agent is not ready yet")
}

// TestRemoteActionNotTakenFailsFast: a forwarded action that no stream takes
// fails at once, with the share's own 503 text.
func TestRemoteActionNotTakenFailsFast(t *testing.T) {
	t.Parallel()
	b, ws := shareTestWorkspace(t, nil)
	owner := shareTestRegister(t, b, ws)
	srv := shareTestStart(t, b, ws, owner)
	check := func(name string) {
		t.Helper()
		start := time.Now()
		action, reply := shareTestAction(t, ShareAction{Kind: ShareYolo})
		srv.offer(t, action)
		require.EqualError(t, shareTestRecv(t, reply, 100*time.Millisecond), "the Crush window is not responding", name)
		require.Less(t, time.Since(start), 100*time.Millisecond, name)
	}

	check("no stream")

	shareTestSubscribe(t, b, ws, owner)
	shareTestFill(b, ws, owner, clientStreamCapacity)
	check("full stream")
}

// TestRemoteActionTimeout: a forwarded action the window never finishes, and
// an engine action that hangs, both get the engine's answer before the
// share's own 30 s.
func TestRemoteActionTimeout(t *testing.T) {
	t.Parallel()
	coord := newShareTestCoordinator()
	coord.holdInterrupts()
	t.Cleanup(coord.releaseInterrupts)
	b, ws := shareTestWorkspace(t, coord)
	ws.share.timeout = 200 * time.Millisecond
	owner, events := shareTestApp(t, b, ws)
	shareTestView(t, b, ws, owner, "chat-1")
	srv := shareTestStart(t, b, ws, owner)

	start := time.Now()
	forwarded, forwardedReply := shareTestAction(t, ShareAction{Kind: ShareYolo, SessionID: "chat-1"})
	srv.offer(t, forwarded)
	ev := shareTestWaitEvent(t, events, pubsub.PayloadTypeRemoteAction)
	actionID := ev.Payload.(proto.RemoteAction).ActionID
	hanging, hangingReply := shareTestAction(t, ShareAction{Kind: ShareInterrupt, SessionID: "chat-1"})
	srv.offer(t, hanging)

	require.EqualError(t, shareTestReply(t, forwardedReply), "the Crush window did not answer")
	require.EqualError(t, shareTestReply(t, hangingReply), "the Crush engine did not answer")
	elapsed := time.Since(start)
	require.GreaterOrEqual(t, elapsed, 200*time.Millisecond, "the answers wait for the timer")
	require.Less(t, elapsed, 30*time.Second)

	// A done after the timer finds the action gone.
	require.ErrorIs(t, b.FinishRemoteAction(ws.ID, owner, actionID, proto.RemoteActionDone{}), ErrActionNotFound)
}

// TestRemotePumpNeverBlocks: the engine takes every action at once, even
// while an earlier one still runs.
func TestRemotePumpNeverBlocks(t *testing.T) {
	t.Parallel()
	coord := newShareTestCoordinator()
	coord.holdInterrupts()
	t.Cleanup(coord.releaseInterrupts)
	b, ws := shareTestWorkspace(t, coord)
	owner, _ := shareTestApp(t, b, ws)
	shareTestView(t, b, ws, owner, "chat-1")
	srv := shareTestStart(t, b, ws, owner)

	slow, slowReply := shareTestAction(t, ShareAction{Kind: ShareInterrupt, SessionID: "chat-1"})
	second, secondReply := shareTestAction(t, ShareAction{Kind: ShareClearQueue, SessionID: "chat-1"})
	third, _ := shareTestAction(t, ShareAction{Kind: ShareYolo, SessionID: "chat-1"})
	start := time.Now()
	srv.offer(t, slow)
	srv.offer(t, second)
	srv.offer(t, third)
	require.Less(t, time.Since(start), 100*time.Millisecond, "all three are taken at once")

	require.NoError(t, shareTestReply(t, secondReply))
	select {
	case err := <-slowReply:
		t.Fatalf("the held action answered early: %v", err)
	default:
	}
	coord.releaseInterrupts()
	require.NoError(t, shareTestReply(t, slowReply))
}

// TestRemoteShareStopsWhenOwnerLeaves: when the owner leaves, its share
// stops and its waiting actions fail with "the Crush window closed".
func TestRemoteShareStopsWhenOwnerLeaves(t *testing.T) {
	t.Parallel()
	t.Run("another app stays", func(t *testing.T) {
		t.Parallel()
		b, ws := shareTestWorkspace(t, nil)
		owner, events := shareTestApp(t, b, ws)
		other, _ := shareTestApp(t, b, ws)
		srv := shareTestStart(t, b, ws, owner)
		action, reply := shareTestAction(t, ShareAction{Kind: ShareYolo})
		srv.offer(t, action)
		shareTestWaitEvent(t, events, pubsub.PayloadTypeRemoteAction)

		require.NoError(t, b.RetireClient(owner))
		require.EqualError(t, shareTestReply(t, reply), "the Crush window closed")
		require.EqualValues(t, 1, srv.stops.Load())
		ws.share.mu.Lock()
		require.Empty(t, ws.share.shares)
		ws.share.mu.Unlock()
		status, err := b.RemoteStatus(ws.ID, other)
		require.NoError(t, err)
		require.Equal(t, remoteStateOff, status.State)
	})
	t.Run("last app", func(t *testing.T) {
		t.Parallel()
		b, ws := shareTestWorkspace(t, nil)
		owner, events := shareTestApp(t, b, ws)
		srv := shareTestStart(t, b, ws, owner)
		action, reply := shareTestAction(t, ShareAction{Kind: ShareMode})
		srv.offer(t, action)
		shareTestWaitEvent(t, events, pubsub.PayloadTypeRemoteAction)

		require.NoError(t, b.RetireClient(owner))
		require.EqualError(t, shareTestReply(t, reply), "the Crush window closed")
		require.EqualValues(t, 1, srv.stops.Load())
	})
}
