package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/fallback"
	"github.com/charmbracelet/crush/internal/jsonfile"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

// writePendingTestConfig writes a minimal project config into workingDir.
func writePendingTestConfig(t *testing.T, workingDir string) {
	t.Helper()
	crushJSON := `{
  "options": {"disable_default_providers": true, "disable_provider_auto_update": true},
  "providers": {"mock": {"id": "mock", "name": "Mock", "type": "openai",
    "base_url": "http://127.0.0.1:9/v1", "api_key": "test-key",
    "models": [{"id": "mock-model", "name": "Mock", "context_window": 8192, "default_max_tokens": 128}]}},
  "models": {"large": {"provider": "mock", "model": "mock-model"},
             "small": {"provider": "mock", "model": "mock-model"}}
}`
	require.NoError(t, os.WriteFile(filepath.Join(workingDir, "crush.json"), []byte(crushJSON), 0o644))
}

// pendingTestCoordinator builds an interactive coordinator in env's folder,
// with the fake clock driving its waits.
func pendingTestCoordinator(t *testing.T, env fakeEnv, clock *fakeClock) *coordinator {
	t.Helper()
	writePendingTestConfig(t, env.workingDir)
	cfg, err := config.Init(env.workingDir, "", false)
	require.NoError(t, err)
	c := &coordinator{cfg: cfg, sessions: env.sessions, messages: env.messages, interactive: true}
	c.limitRetries.now, c.limitRetries.poll = clock.Now, time.Millisecond
	c.fallback.now = clock.Now
	return c
}

func pendingTestSession(t *testing.T, env fakeEnv, title string) string {
	t.Helper()
	sess, err := env.sessions.Create(t.Context(), title)
	require.NoError(t, err)
	return sess.ID
}

// readPendingFile reads the project's saved pending work.
func readPendingFile(t *testing.T, c *coordinator) pendingWork {
	t.Helper()
	return jsonfile.Read[pendingWork](c.savedDataPath(savedPendingFile))
}

// A wait planned before an engine restart is still there afterwards: the
// next Crush waits for the same time and continues the chat then.
func TestSavedWaitSurvivesAnEngineRestart(t *testing.T) {
	env := testEnv(t)
	clock := &fakeClock{now: time.Now()}
	first := pendingTestCoordinator(t, env, clock)
	sessionID := pendingTestSession(t, env, "waiting")

	at := clock.Now().Add(10 * time.Minute)
	var sent atomic.Int32
	first.planWait(savedWait{SessionID: sessionID, At: at, Kind: waitKindLimit}, func() { sent.Add(1) })

	saved := readPendingFile(t, first)
	require.Len(t, saved.Waits, 1, "the wait is saved")
	require.True(t, saved.Waits[0].At.Equal(at))

	// The engine goes; a new one reads the same folder.
	first.limitRetries.drop(sessionID)
	next := pendingTestCoordinator(t, env, clock)
	next.fallback.send = func(string) { sent.Add(1) }
	next.restoreWaits()

	time.Sleep(20 * time.Millisecond)
	require.Zero(t, sent.Load(), "nothing sent before the time")
	clock.Set(at)
	require.Eventually(t, func() bool { return sent.Load() == 1 }, 2*time.Second, time.Millisecond)
}

// A wait whose time passed while Crush was closed continues the chat at
// once, with no extra line in it.
func TestSavedWaitWhoseTimePassedSendsAtStart(t *testing.T) {
	env := testEnv(t)
	clock := &fakeClock{now: time.Now()}
	first := pendingTestCoordinator(t, env, clock)
	sessionID := pendingTestSession(t, env, "late")
	first.planWait(savedWait{SessionID: sessionID, At: clock.Now().Add(time.Minute), Kind: waitKindLimit}, func() {})
	first.limitRetries.drop(sessionID)

	clock.Set(clock.Now().Add(2 * time.Hour))
	next := pendingTestCoordinator(t, env, clock)
	var sent atomic.Int32
	next.fallback.send = func(string) { sent.Add(1) }
	next.restoreWaits()

	require.Eventually(t, func() bool { return sent.Load() == 1 }, 2*time.Second, time.Millisecond)
	msgs, err := env.messages.List(t.Context(), sessionID)
	require.NoError(t, err)
	for _, m := range msgs {
		require.NotContains(t, m.Content().String(), "restarted", "no line is added")
	}
}

// A wait another Crush in the same folder still runs is left alone.
func TestSavedWaitOfALiveCrushIsLeftAlone(t *testing.T) {
	env := testEnv(t)
	clock := &fakeClock{now: time.Now()}
	c := pendingTestCoordinator(t, env, clock)
	sessionID := pendingTestSession(t, env, "owned")
	// Init is alive, and is not the Crush that saved the wait.
	require.NoError(t, c.updatePending(func(p *pendingWork) bool {
		p.Waits = []savedWait{{SessionID: sessionID, At: clock.Now().Add(time.Minute), Kind: waitKindLimit, Owner: 1}}
		return true
	}))
	var sent atomic.Int32
	c.fallback.send = func(string) { sent.Add(1) }
	c.restoreWaits()

	time.Sleep(30 * time.Millisecond)
	require.Zero(t, sent.Load())
	require.Len(t, readPendingFile(t, c).Waits, 1, "the wait stays saved for that Crush")
}

// A wait older than the resume window is dropped instead of sent late.
func TestOldSavedWaitIsDropped(t *testing.T) {
	env := testEnv(t)
	clock := &fakeClock{now: time.Now()}
	c := pendingTestCoordinator(t, env, clock)
	sessionID := pendingTestSession(t, env, "stale")
	require.NoError(t, c.updatePending(func(p *pendingWork) bool {
		p.Waits = []savedWait{{SessionID: sessionID, At: clock.Now().Add(-2 * resumeWindow), Kind: waitKindLimit, Owner: deadPID(t)}}
		return true
	}))
	var sent atomic.Int32
	c.fallback.send = func(string) { sent.Add(1) }
	c.restoreWaits()

	time.Sleep(30 * time.Millisecond)
	require.Zero(t, sent.Load())
	require.Empty(t, readPendingFile(t, c).Waits)
}

// Typing in the chat drops the saved wait, as it drops the planned one.
func TestNewTurnClearsTheSavedWait(t *testing.T) {
	env := testEnv(t)
	clock := &fakeClock{now: time.Now()}
	c := pendingTestCoordinator(t, env, clock)
	sessionID := pendingTestSession(t, env, "typed")
	c.planWait(savedWait{SessionID: sessionID, At: clock.Now().Add(time.Hour), Kind: waitKindLimit}, func() {})
	require.Len(t, readPendingFile(t, c).Waits, 1)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := c.Run(ctx, sessionID, "hello")
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, readPendingFile(t, c).Waits)
}

// The accounts the fallback order counted as out, and the planned way back,
// come back after a restart.
func TestSavedFallbackMemoryComesBack(t *testing.T) {
	env := testEnv(t)
	boise, err := time.LoadLocation("America/Boise")
	require.NoError(t, err)
	clock := &fakeClock{now: time.Date(2026, time.October, 19, 3, 0, 0, 0, boise)}
	first := pendingTestCoordinator(t, env, clock)
	acct := fallback.Account{Kind: config.TypeClaudeCode, ID: "test-account"}
	first.markOut(acct, errors.New("You've hit your session limit · resets 5:20am (America/Boise)"), clock.Now())
	first.fallback.mu.Lock()
	first.fallback.ret = map[string]*fallback.Return{"chat-1": {Block: 1, At: clock.Now().Add(3 * time.Hour)}}
	first.fallback.mu.Unlock()
	first.saveFallbackMemory()

	next := pendingTestCoordinator(t, env, clock)
	next.loadFallbackMemory()
	reading := next.fallbackReadings(clock.Now(), nil)(acct)
	require.True(t, reading.Known)
	require.EqualValues(t, 100, reading.Used)
	require.True(t, reading.OpenAt.Equal(time.Date(2026, 10, 19, 5, 20, 0, 0, boise)), reading.OpenAt)
	next.fallback.mu.Lock()
	ret := next.fallback.ret["chat-1"]
	next.fallback.mu.Unlock()
	require.NotNil(t, ret)
	require.Equal(t, 1, ret.Block)
}

// A prompt typed while a chat was busy is still there after a restart, and
// runs.
func TestSavedQueuedPromptRunsAfterRestart(t *testing.T) {
	c, sessionID := queueTestCoordinator(t)
	// Init is alive and owns nothing here: the Crush that queued it is gone.
	require.NoError(t, c.updatePending(func(p *pendingWork) bool {
		p.Queue = []savedPrompt{{SessionID: sessionID, Prompt: "queued while busy", Owner: deadPID(t)}}
		return true
	}))

	c.restorePrompts()

	require.Eventually(t, func() bool {
		msgs, err := c.messages.List(t.Context(), sessionID)
		if err != nil {
			return false
		}
		for _, m := range msgs {
			if m.Role == message.User && m.Content().String() == "queued while busy" {
				return true
			}
		}
		return false
	}, 10*time.Second, 10*time.Millisecond)
	require.Empty(t, readPendingFile(t, c).Queue, "the entry is claimed once")
}

// A queue this Crush saved itself is not taken from the file: its prompts
// are still in this Crush's agent queue, and running them again repeats
// them, doubling the queue at every look until the chat fills with copies.
func TestPromptsOfThisCrushAreLeftAlone(t *testing.T) {
	c, sessionID := queueTestCoordinator(t)
	require.NoError(t, c.updatePending(func(p *pendingWork) bool {
		p.Queue = []savedPrompt{{SessionID: sessionID, Prompt: "still queued", Owner: os.Getpid()}}
		return true
	}))

	claimed, err := c.claimPrompts()
	require.NoError(t, err)
	require.Empty(t, claimed, "this Crush's own queue is not replayed")
	require.Len(t, readPendingFile(t, c).Queue, 1, "the entry stays saved for it")
}

// A workspace shutdown releases the chats' queued prompts instead of
// dropping them: the agent's own cancel empties its queue, and the second
// shutdown pass must not erase the saved copy. A prompt saved long ago is
// not run.
func TestQueuedPromptsSurviveAWorkspaceShutdown(t *testing.T) {
	c, sessionID := queueTestCoordinator(t)
	scheduler, ok := c.mainAgent.(*sessionAgent)
	require.True(t, ok)
	scheduler.enqueueCall(SessionAgentCall{SessionID: sessionID, Prompt: "waiting here"})
	h := &taskHub{c: c, queueStop: make(chan struct{}), cancels: map[string]context.CancelFunc{}, tasks: map[string]*Task{}}

	h.stopAll()
	scheduler.ClearQueue(sessionID) // as the agent's CancelAll does
	h.stopAll()                     // App.Shutdown cancels everything again
	require.NoError(t, c.updatePending(func(p *pendingWork) bool {
		p.Queue = append(p.Queue, savedPrompt{SessionID: sessionID, Prompt: "old", Owner: deadPID(t), Saved: time.Now().Add(-2 * resumeWindow)})
		return true
	}))

	claimed, err := c.claimPrompts()
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, "waiting here", claimed[0].Prompt)
	require.Empty(t, readPendingFile(t, c).Queue)
}

// A wait whose time comes during a shutdown stays saved for the next
// engine, and is not sent by the one closing.
func TestShutdownKeepsAPlannedWait(t *testing.T) {
	c, sessionID := queueTestCoordinator(t)
	clock := &fakeClock{now: time.Now()}
	c.limitRetries.now, c.limitRetries.poll = clock.Now, time.Millisecond
	c.tasks = &taskHub{c: c, queueStop: make(chan struct{}), cancels: map[string]context.CancelFunc{}, tasks: map[string]*Task{}}
	at := clock.Now().Add(time.Minute)
	var sent atomic.Int32
	c.planWait(savedWait{SessionID: sessionID, At: at, Kind: waitKindLimit}, func() { sent.Add(1) })

	c.CancelAll()
	clock.Set(at)
	time.Sleep(50 * time.Millisecond)

	require.Zero(t, sent.Load())
	require.Len(t, readPendingFile(t, c).Waits, 1, "the next engine continues it")
}

// queueTestCoordinator is an interactive coordinator whose agent answers
// with a fixed reply, for the queued-prompt tests.
func queueTestCoordinator(t *testing.T) (*coordinator, string) {
	t.Helper()
	c := newGateTestCoordinator(t, true)
	require.NoError(t, c.readyWg.Wait())
	env := fakeEnv{sessions: c.sessions, messages: c.messages}
	sa := testSessionAgent(env, &finishStreamModel{text: "ran"}, &finishStreamModel{text: "title"}, "system")
	limits := catwalk.Model{ContextWindow: 200000, DefaultMaxTokens: 10000}
	selected := config.SelectedModel{Provider: "mock", Model: "mock-model"}
	sa.SetModels(Model{Model: &finishStreamModel{text: "ran"}, CatwalkCfg: limits, ModelCfg: selected},
		Model{Model: &finishStreamModel{text: "title"}, CatwalkCfg: limits, ModelCfg: selected})
	c.mainAgent = sa
	c.agents[config.AgentCoder] = sa
	sess, err := c.sessions.Create(t.Context(), "queued")
	require.NoError(t, err)
	return c, sess.ID
}

// A prompt typed while a chat was busy is saved while it waits, and its
// entry goes when the turn picks it up.
func TestQueuedPromptIsSavedWhileItWaits(t *testing.T) {
	c, sessionID := queueTestCoordinator(t)
	scheduler, ok := c.mainAgent.(*sessionAgent)
	require.True(t, ok)

	scheduler.enqueueCall(SessionAgentCall{SessionID: sessionID, Prompt: "waiting here"})
	h := &taskHub{c: c}
	h.queueStop = make(chan struct{})
	h.writeQueues(false)

	saved := readPendingFile(t, c)
	require.Len(t, saved.Queue, 1)
	require.Equal(t, "waiting here", saved.Queue[0].Prompt)

	scheduler.ClearQueue(sessionID)
	h.writeQueues(false)
	require.Empty(t, readPendingFile(t, c).Queue)
}

// The engine's start brings the waits and queued prompts of the Crush it
// replaced back, the same way it resumes interrupted helpers.
func TestResumeBringsBackPendingWork(t *testing.T) {
	c, sessionID := queueTestCoordinator(t)
	clock := &fakeClock{now: time.Now()}
	c.limitRetries.now, c.limitRetries.poll = clock.Now, time.Millisecond
	c.fallback.now = clock.Now
	var sent atomic.Int32
	c.fallback.send = func(string) { sent.Add(1) }

	// The prompt belongs to another chat: a prompt in the waiting chat
	// takes the chat over, which drops its wait, as typing does.
	queuedChat, err := c.sessions.Create(t.Context(), "queued")
	require.NoError(t, err)
	// The wait ended while the engine was down, so it continues at once.
	at := clock.Now().Add(-time.Second)
	require.NoError(t, c.updatePending(func(p *pendingWork) bool {
		p.Waits = []savedWait{{SessionID: sessionID, At: at, Kind: waitKindLimit, Owner: deadPID(t)}}
		p.Queue = []savedPrompt{{SessionID: queuedChat.ID, Prompt: "queued while busy", Owner: deadPID(t)}}
		return true
	}))

	h := &taskHub{c: c, queueStop: make(chan struct{}), cancels: map[string]context.CancelFunc{}, tasks: map[string]*Task{}}
	h.resumeInterrupted(t.Context())

	require.Eventually(t, func() bool { return sent.Load() == 1 }, 2*time.Second, time.Millisecond)
	require.Empty(t, readPendingFile(t, c).Waits)
	require.Empty(t, readPendingFile(t, c).Queue)
	require.Eventually(t, func() bool {
		msgs, err := c.messages.List(t.Context(), queuedChat.ID)
		return err == nil && len(msgs) > 0
	}, 10*time.Second, 10*time.Millisecond, "the queued prompt ran")
}
