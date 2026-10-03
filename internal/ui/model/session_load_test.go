package model

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/history"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/attachments"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/charmbracelet/crush/internal/ui/completions"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/util"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// loadWorkspace serves transcripts like a slow server: ListMessages for a
// session waits on its gate (if any) and nested agent transcripts take
// nestedDelay each. It records any transcript read made while the test is
// inside Update, which must never happen.
type loadWorkspace struct {
	testWorkspace

	mu          sync.Mutex
	transcripts map[string][]message.Message
	gates       map[string]chan struct{}
	errs        map[string]error
	nestedDelay time.Duration
	serial      sync.Mutex
	runSessions []string

	inUpdate      atomic.Bool
	readsInUpdate atomic.Int32
	reads         atomic.Int32
}

func newLoadWorkspace() *loadWorkspace {
	return &loadWorkspace{
		testWorkspace: testWorkspace{
			cfg:        &config.Config{Providers: csync.NewMap[string, config.ProviderConfig](), Options: &config.Options{}},
			agentReady: true,
		},
		transcripts: make(map[string][]message.Message),
		gates:       make(map[string]chan struct{}),
		errs:        make(map[string]error),
	}
}

func (w *loadWorkspace) gate(sessionID string) chan struct{} {
	w.mu.Lock()
	defer w.mu.Unlock()
	g := make(chan struct{})
	w.gates[sessionID] = g
	return g
}

func (w *loadWorkspace) GetSession(_ context.Context, id string) (session.Session, error) {
	return session.Session{ID: id, Title: "Title of " + id}, nil
}

func (w *loadWorkspace) ListSessionHistory(context.Context, string) ([]history.File, error) {
	return nil, nil
}

func (w *loadWorkspace) FileTrackerListReadFiles(context.Context, string) ([]string, error) {
	return nil, nil
}

func (w *loadWorkspace) SetCurrentSession(context.Context, string) error { return nil }

func (w *loadWorkspace) LSPStart(context.Context, string) {}

func (w *loadWorkspace) CreateAgentToolSessionID(messageID, toolCallID string) string {
	return messageID + "$$" + toolCallID
}

func (w *loadWorkspace) ParseAgentToolSessionID(sessionID string) (string, string, bool) {
	messageID, toolCallID, ok := strings.Cut(sessionID, "$$")
	return messageID, toolCallID, ok
}

func (w *loadWorkspace) ListMessages(ctx context.Context, sessionID string) ([]message.Message, error) {
	w.reads.Add(1)
	if w.inUpdate.Load() {
		w.readsInUpdate.Add(1)
	}
	w.mu.Lock()
	g, msgs, err := w.gates[sessionID], w.transcripts[sessionID], w.errs[sessionID]
	w.mu.Unlock()
	if g != nil {
		select {
		case <-g:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if strings.Contains(sessionID, "$$") && w.nestedDelay > 0 {
		// One read at a time, like the single database connection, so
		// the old and new paths do the same total read work.
		w.serial.Lock()
		time.Sleep(w.nestedDelay)
		w.serial.Unlock()
	}
	return msgs, err
}

func (w *loadWorkspace) AgentRun(_ context.Context, sessionID, prompt string, _ ...message.Attachment) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.runSessions = append(w.runSessions, sessionID)
	w.runPrompts = append(w.runPrompts, prompt)
	return nil
}

func newLoadTestUI(t testing.TB, ws *loadWorkspace) *UI {
	t.Helper()
	u := newTestUI()
	u.com.Workspace = ws
	u.frames = newFrameCache(time.Minute, 8)
	u.dialog = dialog.NewOverlay()
	u.keyMap = DefaultKeyMap()
	u.status = NewStatus(u.com, u)
	u.attachments = attachments.New(nil, attachments.Keymap{})
	u.completions = completions.New(u.com.Styles.Completions.Normal, u.com.Styles.Completions.Focused, u.com.Styles.Completions.Match)
	u.agentReady = true
	u.session = &session.Session{ID: "old", Title: "Old chat"}
	old := message.Message{ID: "old-user", SessionID: "old", Role: message.User}
	old.AppendContent("a message in the old chat")
	u.chat.SetMessages(chat.ExtractMessageItems(u.com.Styles, &old, nil, "")...)
	u.updateLayoutAndSize()
	return u
}

// update runs one Update the way the event loop does, flagging it so the
// workspace can catch transcript reads on the UI thread.
func update(u *UI, ws *loadWorkspace, msg tea.Msg) tea.Cmd {
	ws.inUpdate.Store(true)
	defer ws.inUpdate.Store(false)
	_, cmd := u.Update(msg)
	return cmd
}

// startCmds runs a command tree concurrently, like the Bubble Tea runtime,
// delivering the messages it produces on out.
func startCmds(cmd tea.Cmd, out chan<- tea.Msg) {
	if cmd == nil {
		return
	}
	go func() {
		switch msg := cmd().(type) {
		case nil:
		case tea.BatchMsg:
			for _, c := range msg {
				startCmds(c, out)
			}
		default:
			out <- msg
		}
	}()
}

// await returns the first message of type T, dropping others.
func await[T tea.Msg](t testing.TB, msgs <-chan tea.Msg) T {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case msg := <-msgs:
			if typed, ok := msg.(T); ok {
				return typed
			}
		case <-timeout:
			var zero T
			t.Fatalf("timed out waiting for %T", zero)
			return zero
		}
	}
}

func userMsg(sessionID, id, text string, at int64) message.Message {
	m := message.Message{ID: id, SessionID: sessionID, Role: message.User, CreatedAt: at}
	m.AppendContent(text)
	return m
}

func assistantMsg(sessionID, id, text string, finished bool) message.Message {
	m := message.Message{ID: id, SessionID: sessionID, Role: message.Assistant}
	m.AppendContent(text)
	if finished {
		m.AddFinish(message.FinishReasonEndTurn, "", "")
	}
	return m
}

// syntheticSession is a long session: per turn a prompt, a step with a
// markdown reply and tool calls (every agentEvery-th one an agent call with
// its own child transcript), the tool results and a final reply.
func syntheticSession(ws *loadWorkspace, sessionID string, turns, agentEvery int) []message.Message {
	body := strings.Repeat("A paragraph of the reply that wraps across a few lines of the terminal. ", 6)
	code := "```go\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n```\n"
	msgs := make([]message.Message, 0, turns*4)
	for i := range turns {
		at := int64(1_700_000_000 + i*60)
		msgs = append(msgs, userMsg(sessionID, fmt.Sprintf("u%d", i), fmt.Sprintf("Question %d: how does the cache get invalidated?", i), at))

		step := message.Message{ID: fmt.Sprintf("s%d", i), SessionID: sessionID, Role: message.Assistant, CreatedAt: at + 1}
		step.AppendContent("## Step\n\n" + body + "\n\n" + code)
		calls := []message.ToolCall{
			{ID: fmt.Sprintf("b%d", i), Name: "bash", Input: `{"command":"go test ./..."}`, Finished: true},
			{ID: fmt.Sprintf("v%d", i), Name: "view", Input: `{"file_path":"/tmp/x.go"}`, Finished: true},
		}
		if agentEvery > 0 && i%agentEvery == 0 {
			call := message.ToolCall{ID: fmt.Sprintf("g%d", i), Name: agent.AgentToolName, Input: `{"prompt":"look around"}`, Finished: true}
			calls = append(calls, call)
			child := ws.CreateAgentToolSessionID(step.ID, call.ID)
			nested := message.Message{ID: child + "-a", SessionID: child, Role: message.Assistant}
			nested.Parts = append(nested.Parts,
				message.ToolCall{ID: child + "-grep", Name: "grep", Input: `{"pattern":"cache"}`, Finished: true},
				message.ToolCall{ID: child + "-view", Name: "view", Input: `{"file_path":"/tmp/y.go"}`, Finished: true})
			results := message.Message{ID: child + "-t", SessionID: child, Role: message.Tool, Parts: []message.ContentPart{
				message.ToolResult{ToolCallID: child + "-grep", Name: "grep", Content: "found"},
				message.ToolResult{ToolCallID: child + "-view", Name: "view", Content: "package y"},
			}}
			ws.transcripts[child] = []message.Message{nested, results}
		}
		for _, c := range calls {
			step.Parts = append(step.Parts, c)
		}
		step.AddFinish(message.FinishReasonToolUse, "", "")
		msgs = append(msgs, step)

		results := message.Message{ID: fmt.Sprintf("t%d", i), SessionID: sessionID, Role: message.Tool}
		for _, c := range calls {
			results.Parts = append(results.Parts, message.ToolResult{ToolCallID: c.ID, Name: c.Name, Content: strings.Repeat("output line\n", 20)})
		}
		msgs = append(msgs, results)
		msgs = append(msgs, assistantMsg(sessionID, fmt.Sprintf("f%d", i), "Done: "+body, true))
	}
	ws.transcripts[sessionID] = msgs
	return msgs
}

// TestSessionSwitchShowsAnimatedIndicatorWhileLoading opens a session whose
// transcript read blocks: the switch must show its loading line at once,
// animate it, stay responsive to keys, and keep the old chat untouched by
// fetches on the UI thread.
func TestSessionSwitchShowsAnimatedIndicatorWhileLoading(t *testing.T) {
	ws := newLoadWorkspace()
	syntheticSession(ws, "big", 40, 10)
	release := ws.gate("big")
	u := newLoadTestUI(t, ws)

	msgs := make(chan tea.Msg, 64)
	ws.inUpdate.Store(true)
	cmd := u.openSession(session.Session{ID: "big", Title: "Big chat"})
	ws.inUpdate.Store(false)
	startCmds(cmd, msgs)

	require.True(t, u.sessionSwitchPending())
	first := u.View().Content
	require.Contains(t, first, "Opening Big chat…")
	require.NotContains(t, first, "a message in the old chat", "the old chat must not stay on screen as if it were the new one")

	// The indicator animates: each tick advances it and re-arms the next.
	// (The Update tail's own commands probe the workspace stub, so only the
	// tick is re-armed here.)
	for range 3 {
		tick := await[sessionLoadTickMsg](t, msgs)
		require.NotNil(t, update(u, ws, tick))
		startCmds(sessionLoadTick(u.sessionLoad.seq), msgs)
	}
	require.Equal(t, 3, u.sessionLoad.frame)
	require.Len(t, u.handleSessionLoadMsg(sessionLoadTickMsg{seq: u.sessionLoad.seq}), 1, "a live load re-arms its tick")
	require.NotEqual(t, first, u.View().Content, "the spinner must move between ticks")

	// Keys keep flowing while the transcript loads.
	for _, r := range "hi" {
		start := time.Now()
		update(u, ws, tea.KeyPressMsg{Code: r, Text: string(r)})
		u.View()
		require.Less(t, time.Since(start), 100*time.Millisecond)
	}
	require.Equal(t, "hi", u.textarea.Value())

	close(release)
	loaded := await[loadSessionMsg](t, msgs)
	require.NotNil(t, loaded.prepared, "items must be built off the UI thread")
	update(u, ws, loaded)

	require.Zero(t, ws.readsInUpdate.Load(), "Update must never read a transcript")
	require.False(t, u.sessionSwitchPending())
	require.Equal(t, "big", u.session.ID)
	require.NotContains(t, u.View().Content, "Opening Big chat")
	require.NotNil(t, u.chat.MessageItem("f39"), "the last reply is shown")
	require.NotNil(t, u.chat.MessageItem("u0"), "history is kept")
	agentItem, ok := u.chat.MessageItem("g10").(chat.NestedToolContainer)
	require.True(t, ok)
	require.Len(t, agentItem.NestedTools(), 2, "agent calls get their nested tools")

	// Ticks of a finished load stop instead of re-arming.
	require.Nil(t, u.handleSessionLoadMsg(sessionLoadTickMsg{seq: loaded.seq}))
}

// TestSessionSwitchIgnoresStaleCompletion switches twice; the first load
// finishing last must neither replace the second session nor report.
func TestSessionSwitchIgnoresStaleCompletion(t *testing.T) {
	ws := newLoadWorkspace()
	ws.transcripts["a"] = []message.Message{userMsg("a", "ua", "from a", 1)}
	ws.transcripts["b"] = []message.Message{userMsg("b", "ub", "from b", 1)}
	releaseA := ws.gate("a")
	u := newLoadTestUI(t, ws)

	msgsA := make(chan tea.Msg, 64)
	startCmds(u.openSession(session.Session{ID: "a"}), msgsA)
	msgsB := make(chan tea.Msg, 64)
	startCmds(u.openSession(session.Session{ID: "b"}), msgsB)

	update(u, ws, await[loadSessionMsg](t, msgsB))
	require.Equal(t, "b", u.session.ID)
	require.NotNil(t, u.chat.MessageItem("ub"))

	// The superseded load was canceled; whatever it returns is stale.
	close(releaseA)
	var stale tea.Msg
	for stale == nil {
		switch msg := (<-msgsA).(type) {
		case loadSessionMsg, sessionLoadFailedMsg:
			stale = msg
		}
	}
	require.Empty(t, u.handleSessionLoadMsg(stale))
	require.Equal(t, "b", u.session.ID)
	require.Nil(t, u.chat.MessageItem("ua"))
	require.False(t, u.sessionSwitchPending())
}

// TestSessionLoadFailureEndsLoadingAndKeepsPrompt fails the transcript
// read: the indicator must go, the old session stays, the error is
// reported, and a prompt typed meanwhile returns to the editor instead of
// going to the old session.
func TestSessionLoadFailureEndsLoadingAndKeepsPrompt(t *testing.T) {
	ws := newLoadWorkspace()
	ws.errs["broken"] = errors.New("database is locked")
	release := ws.gate("broken")
	u := newLoadTestUI(t, ws)

	msgs := make(chan tea.Msg, 64)
	startCmds(u.openSession(session.Session{ID: "broken"}), msgs)
	require.Nil(t, u.sendMessage("for the new chat"))
	require.Empty(t, u.pendingPrompts, "nothing may be sent while switching")

	close(release)
	failed := await[sessionLoadFailedMsg](t, msgs)
	cmds := u.handleSessionLoadMsg(failed)
	require.False(t, u.sessionSwitchPending())
	require.Equal(t, "old", u.session.ID)
	require.Contains(t, u.View().Content, "a message in the old chat")
	require.Equal(t, "for the new chat", u.textarea.Value())
	require.Empty(t, u.pendingPrompts)
	require.Empty(t, ws.runSessions)

	require.NotEmpty(t, cmds)
	info, ok := cmds[len(cmds)-1]().(util.InfoMsg)
	require.True(t, ok)
	require.Equal(t, util.InfoTypeError, info.Type)
	require.Contains(t, info.Msg, "database is locked")
}

// TestSessionSwitchSendsDeferredPromptToNewSession: a prompt sent during a
// switch goes to the session being opened once it is on screen.
func TestSessionSwitchSendsDeferredPromptToNewSession(t *testing.T) {
	ws := newLoadWorkspace()
	ws.transcripts["new"] = []message.Message{userMsg("new", "u1", "earlier", 1)}
	release := ws.gate("new")
	u := newLoadTestUI(t, ws)

	msgs := make(chan tea.Msg, 64)
	startCmds(u.openSession(session.Session{ID: "new"}), msgs)
	require.Nil(t, u.sendMessage("follow up"))
	require.Empty(t, u.pendingPrompts)

	close(release)
	update(u, ws, await[loadSessionMsg](t, msgs))
	require.Len(t, u.pendingPrompts, 1)
	require.Equal(t, "new", u.pendingPrompts[0].SessionID)
	last, ok := u.chat.flat[len(u.chat.flat)-1].(*chat.UserMessageItem)
	require.True(t, ok)
	require.Equal(t, u.pendingPrompts[0].ID, last.ID(), "the prompt shows at the end of the new chat")
}

// TestEventsDuringLoadAreReplayed streams into the session while it loads:
// new messages land after the snapshot, and an update older than the
// snapshot's copy does not roll it back. The old chat is left alone.
func TestEventsDuringLoadAreReplayed(t *testing.T) {
	ws := newLoadWorkspace()
	ws.transcripts["live"] = []message.Message{
		userMsg("live", "u1", "prompt", 1),
		assistantMsg("live", "a1", "complete answer", true),
	}
	release := ws.gate("live")
	u := newLoadTestUI(t, ws)
	oldLen := u.chat.Len()

	msgs := make(chan tea.Msg, 64)
	startCmds(u.openSession(session.Session{ID: "live"}), msgs)

	partial := assistantMsg("live", "a1", "compl", false)
	update(u, ws, pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: partial})
	update(u, ws, pubsub.Event[message.Message]{Type: pubsub.CreatedEvent, Payload: userMsg("live", "u2", "next prompt", 2)})
	streaming := assistantMsg("live", "a2", "streaming…", false)
	update(u, ws, pubsub.Event[message.Message]{Type: pubsub.CreatedEvent, Payload: streaming})
	for _, text := range []string{"streaming mo", "streaming more"} {
		streaming.Parts = nil
		streaming.AppendContent(text)
		update(u, ws, pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: streaming})
	}
	require.Equal(t, oldLen, u.chat.Len(), "events for the opening session must not touch the old chat")
	require.Len(t, u.sessionLoad.events, 4, "consecutive chunks of one message coalesce")

	close(release)
	update(u, ws, await[loadSessionMsg](t, msgs))
	require.NotNil(t, u.chat.MessageItem("u2"))
	a2, ok := u.chat.MessageItem("a2").(*chat.AssistantMessageItem)
	require.True(t, ok, "a message created during the load must show")
	require.Contains(t, ansi.Strip(a2.RawRender(80)), "streaming more")
	a1, ok := u.chat.MessageItem("a1").(*chat.AssistantMessageItem)
	require.True(t, ok)
	require.Contains(t, ansi.Strip(a1.RawRender(80)), "complete answer", "a stale chunk must not roll back the finished reply")
}

// TestNewChatAbandonsSessionLoad: starting a new chat mid-switch must not
// be undone by the late completion.
func TestNewChatAbandonsSessionLoad(t *testing.T) {
	ws := newLoadWorkspace()
	ws.transcripts["slow"] = []message.Message{userMsg("slow", "u1", "x", 1)}
	release := ws.gate("slow")
	u := newLoadTestUI(t, ws)

	msgs := make(chan tea.Msg, 64)
	startCmds(u.openSession(session.Session{ID: "slow"}), msgs)
	u.newSession()
	require.False(t, u.sessionSwitchPending())
	close(release)
	var late tea.Msg
	for late == nil {
		switch msg := (<-msgs).(type) {
		case loadSessionMsg, sessionLoadFailedMsg:
			late = msg
		}
	}
	require.Empty(t, u.handleSessionLoadMsg(late))
	require.Nil(t, u.session)
}

// TestLongSessionLoadKeepsUIThreadShort measures a representative long
// session (600 turns, 2400 messages, 60 agent calls whose transcripts take
// 15ms each, like an HTTP round trip) through the old synchronous path and
// the new one, and checks the UI thread work and every warm-up step stay
// short.
func TestLongSessionLoadKeepsUIThreadShort(t *testing.T) {
	if testing.Short() {
		t.Skip("measurement")
	}
	ws := newLoadWorkspace()
	ws.nestedDelay = 15 * time.Millisecond
	transcript := syntheticSession(ws, "long", 600, 10)

	// Before: everything (nested reads included) on the UI thread, which is
	// what the loadSessionMsg handler used to do.
	before := newLoadTestUI(t, ws)
	before.session = &session.Session{ID: "long"}
	start := time.Now()
	before.setSessionMessages(transcript)
	before.View()
	syncPath := time.Since(start)

	// After: the load runs off-thread, Update only swaps the items in.
	u := newLoadTestUI(t, ws)
	msgs := make(chan tea.Msg, 256)
	start = time.Now()
	startCmds(u.openSession(session.Session{ID: "long", Title: "Long chat"}), msgs)
	dispatch := time.Since(start)
	loaded := await[loadSessionMsg](t, msgs)
	background := time.Since(start)

	start = time.Now()
	update(u, ws, loaded)
	u.View()
	apply := time.Since(start)
	require.Zero(t, ws.readsInUpdate.Load())

	// Run the cache warm-up the way the event loop would, one step and
	// frame per message.
	var steps int
	var slowest time.Duration
	for u.chat.resizing {
		require.Less(t, steps, 10_000, "warming must finish")
		start = time.Now()
		update(u, ws, chatWarmMsg{seq: u.chat.resizeSettleSeq})
		u.View()
		slowest = max(slowest, time.Since(start))
		steps++
	}
	start = time.Now()
	u.chat.ScrollBy(-40)
	_ = u.chat.list.TotalHeight()
	firstScroll := time.Since(start)

	t.Logf("long session: %d messages, %d chat rows", len(transcript), u.chat.Len())
	t.Logf("old synchronous load on the UI thread: %v", syncPath)
	t.Logf("new: dispatch %v, background load %v, UI-thread apply + first frame %v", dispatch, background, apply)
	t.Logf("new: warm-up %d steps, slowest step + frame %v, first scroll after warm-up %v", steps, slowest, firstScroll)

	require.Less(t, dispatch, 20*time.Millisecond)
	require.Less(t, apply, 250*time.Millisecond)
	require.Less(t, slowest, 100*time.Millisecond)
	require.Less(t, apply, syncPath/2, "the UI thread must do much less than before")
}
