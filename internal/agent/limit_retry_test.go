package agent

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

func TestResetTime(t *testing.T) {
	boise, err := time.LoadLocation("America/Boise")
	require.NoError(t, err)
	london, err := time.LoadLocation("Europe/London")
	require.NoError(t, err)
	// Monday, October 19, 2026, 3:00 AM in Boise.
	now := time.Date(2026, time.October, 19, 3, 0, 0, 0, boise)

	tests := []struct {
		name string
		text string
		now  time.Time
		want time.Time
	}{
		{"claude session limit", "Claude Code: You've hit your session limit · resets 5:20am (America/Boise)", now, time.Date(2026, 10, 19, 5, 20, 0, 0, boise)},
		{"claude time already past today", "Claude Code: You've hit your session limit · resets 5:20am (America/Boise)", now.Add(3*time.Hour + 30*time.Minute), time.Date(2026, 10, 20, 5, 20, 0, 0, boise)},
		{"claude time a few seconds late", "resets 5:20am (America/Boise)", time.Date(2026, 10, 19, 5, 20, 40, 0, boise), time.Date(2026, 10, 19, 5, 20, 0, 0, boise)},
		{"claude other zone", "resets 5pm (Europe/London)", now, time.Date(2026, 10, 19, 17, 0, 0, 0, london)},
		{"claude weekly with date", "Claude Code: You've hit your weekly limit · resets Oct 22, 9am (America/Boise)", now, time.Date(2026, 10, 22, 9, 0, 0, 0, boise)},
		{"weekday", "resets Thu 9am (America/Boise)", now, time.Date(2026, 10, 22, 9, 0, 0, 0, boise)},
		{"noon and midnight", "resets 12am", now, time.Date(2026, 10, 20, 0, 0, 0, 0, boise)},
		{"codex same day", "Codex: You've hit your usage limit. Upgrade to Pro (https://chatgpt.com/explore/pro) or try again at 5:20 PM.", now, time.Date(2026, 10, 19, 17, 20, 0, 0, boise)},
		{"codex other day", "Codex: You've hit your usage limit. Try again at Oct 20th, 2026 5:20 AM.", now, time.Date(2026, 10, 20, 5, 20, 0, 0, boise)},
		{"wait", "Rate limited. Try again in 2 hours 5 minutes.", now, now.Add(2*time.Hour + 5*time.Minute)},
		{"short wait", "retry in 1h 30m", now, now.Add(90 * time.Minute)},
		{"timestamp", "Usage limit: resets at 2026-10-20T05:20:00Z", now, time.Date(2026, 10, 20, 5, 20, 0, 0, time.UTC)},
		{"old claude stamp", "Claude AI usage limit reached|1792400000", now, time.Unix(1792400000, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := resetTime(tt.text, tt.now)
			require.True(t, ok)
			require.True(t, tt.want.Equal(got), "want %v, got %v", tt.want, got)
		})
	}

	for _, text := range []string{
		"read tcp: connection reset by peer",
		"Provider Error: overloaded, retry 3 times",
		"Claude Code: You've hit your session limit",
		"resets 25:00",
		"",
	} {
		_, ok := resetTime(text, now)
		require.False(t, ok, text)
	}
}

func TestLimitRetryTime(t *testing.T) {
	boise, err := time.LoadLocation("America/Boise")
	require.NoError(t, err)
	now := time.Date(2026, time.October, 19, 3, 0, 0, 0, boise)

	at, ok := limitRetryTime(errors.New("Claude Code: You've hit your session limit · resets 5:20am (America/Boise)"), now)
	require.True(t, ok)
	require.True(t, at.Equal(time.Date(2026, 10, 19, 5, 21, 0, 0, boise)), at)

	// A reset already past still waits a minute.
	at, ok = limitRetryTime(errors.New("resets 2:50am (America/Boise)"), now)
	require.True(t, ok)
	require.True(t, at.Equal(now.Add(time.Minute)), at)

	_, ok = limitRetryTime(errors.New("try again in 9 days"), now)
	require.False(t, ok, "longer than a weekly limit")
	_, ok = limitRetryTime(context.Canceled, now)
	require.False(t, ok)
	_, ok = limitRetryTime(nil, now)
	require.False(t, ok)

	require.Contains(t, limitRetryNote(now), `Crush will send "continue" at `)
}

// fakeClock is a wall clock tests move by hand.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

func TestLimitRetriesSendAtTheTime(t *testing.T) {
	clock := &fakeClock{now: time.Now()}
	r := &limitRetries{now: clock.Now, poll: time.Millisecond}
	var sent atomic.Int32
	at := clock.Now().Add(5 * time.Minute)
	r.plan("s", at, func() { sent.Add(1) })

	time.Sleep(20 * time.Millisecond)
	require.Zero(t, sent.Load(), "sent before the time")
	clock.Set(at)
	require.Eventually(t, func() bool { return sent.Load() == 1 }, 2*time.Second, time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	require.EqualValues(t, 1, sent.Load(), "sent more than once")
}

func TestLimitRetriesDropAndReplace(t *testing.T) {
	clock := &fakeClock{now: time.Now()}
	r := &limitRetries{now: clock.Now, poll: time.Millisecond}
	var dropped, first, second atomic.Int32
	at := clock.Now().Add(time.Minute)
	r.plan("dropped", at, func() { dropped.Add(1) })
	r.drop("dropped")
	r.plan("replaced", at, func() { first.Add(1) })
	r.plan("replaced", at, func() { second.Add(1) })

	clock.Set(at)
	require.Eventually(t, func() bool { return second.Load() == 1 }, 2*time.Second, time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	require.Zero(t, dropped.Load())
	require.Zero(t, first.Load())
}

// Anything new in a chat, like the user typing, drops its planned retry.
func TestCoordinatorRunDropsLimitRetry(t *testing.T) {
	clock := &fakeClock{now: time.Now()}
	c := &coordinator{interactive: true}
	c.limitRetries.now, c.limitRetries.poll = clock.Now, time.Millisecond
	var sent atomic.Int32
	at := clock.Now().Add(time.Minute)
	c.limitRetries.plan("s", at, func() { sent.Add(1) })

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := c.Run(ctx, "s", "hello")
	require.ErrorIs(t, err, context.Canceled)

	clock.Set(at)
	time.Sleep(20 * time.Millisecond)
	require.Zero(t, sent.Load())
}

// Only interactive chats plan a retry; helpers and headless runs don't.
func TestLimitRetryOnlyInteractive(t *testing.T) {
	limitErr := errors.New("Claude Code: You've hit your session limit · try again in 2 hours")
	headless := &coordinator{}
	headless.planLimitRetry("s", limitErr)
	require.Empty(t, headless.limitRetries.pending)

	chat := &coordinator{interactive: true}
	chat.planLimitRetry("s", limitErr)
	require.Len(t, chat.limitRetries.pending, 1)
	chat.limitRetries.drop("s")
}

// A chat whose Claude Code turn hits the session limit says when it will
// continue, then sends "continue" by itself once the limit resets.
func TestChatContinuesAfterUsageLimitResets(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is required for the fake native CLI")
	}
	dir := t.TempDir()
	// A reset two hours ahead, whatever the time of day: one named for an
	// hour that has just passed is retried a minute from now instead
	// (limitRetryTime), so a fixed hour failed for an hour every morning.
	boise, err := time.LoadLocation("America/Boise")
	require.NoError(t, err)
	reset := time.Now().In(boise).Add(2 * time.Hour).Truncate(time.Minute)
	resetText := strings.ToLower(reset.Format("3:04pm"))
	script := `#!` + python + `
import json, os
root = os.environ["CRUSH_LIMIT_FIXTURE"]
def read():
    data = b""
    while not data.endswith(b"\n"):
        byte = os.read(0, 1)
        if not byte:
            raise SystemExit(0)
        data += byte
    return data.decode()
def emit(value):
    print(json.dumps(value), flush=True)
read() # Initialization control request.
while True:
    read() # A prompt.
    count = os.path.join(root, "prompts")
    n = int(open(count).read()) + 1 if os.path.exists(count) else 1
    open(count, "w").write(str(n))
    emit({"type":"system","subtype":"init","session_id":"limit"})
    if n == 1:
        emit({"type":"result","subtype":"success","is_error":True,"result":"You've hit your session limit · resets ` + resetText + ` (America/Boise)"})
    else:
        emit({"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"picked up"}}})
        emit({"type":"result","subtype":"success","result":"picked up"})
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	provider := cliagent.NewProvider(config.TypeClaudeCode, dir, dir, nil, nil, "", false, "")
	model, err := provider.LanguageModel(t.Context(), "fixture")
	require.NoError(t, err)
	model.(*cliagent.Model).Env = []string{"CRUSH_LIMIT_FIXTURE=" + dir}

	c := newGateTestCoordinator(t, true)
	require.NoError(t, c.readyWg.Wait())
	env := fakeEnv{sessions: c.sessions, messages: c.messages}
	sa := testSessionAgent(env, model, &finishStreamModel{text: "title"}, "system")
	limits := catwalk.Model{ContextWindow: 200000, DefaultMaxTokens: 10000}
	selected := config.SelectedModel{Provider: "mock", Model: "mock-model"}
	sa.SetModels(Model{Model: model, CatwalkCfg: limits, ModelCfg: selected}, Model{Model: &finishStreamModel{text: "title"}, CatwalkCfg: limits, ModelCfg: selected})
	c.mainAgent = fixedModelsAgent{sa}
	clock := &fakeClock{now: time.Now()}
	c.limitRetries.now, c.limitRetries.poll = clock.Now, time.Millisecond
	sess, err := c.sessions.Create(t.Context(), "limit")
	require.NoError(t, err)
	t.Cleanup(func() { _ = cliagent.CloseForTree(sess.ID) })

	_, err = c.Run(t.Context(), sess.ID, "do the work")
	require.ErrorContains(t, err, "resets "+resetText)

	msgs, err := c.messages.List(t.Context(), sess.ID)
	require.NoError(t, err)
	finish := msgs[len(msgs)-1].FinishPart()
	require.NotNil(t, finish)
	// A minute past the reset, shown in this computer's own time zone.
	shown := reset.Add(time.Minute).Local().Format("3:04 PM")
	require.Contains(t, finish.Details, `Crush will send "continue" at `+shown)
	require.Len(t, c.limitRetries.pending, 1)

	// Nothing is sent before the reset; past it, "continue" goes out.
	time.Sleep(20 * time.Millisecond)
	msgs, err = c.messages.List(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	clock.Set(time.Now().Add(maxLimitWait))
	require.Eventually(t, func() bool {
		msgs, err := c.messages.List(t.Context(), sess.ID)
		if err != nil || len(msgs) < 4 {
			return false
		}
		user, reply := msgs[2], msgs[3]
		return user.Role == message.User && user.Content().String() == "continue" &&
			reply.FinishReason() == message.FinishReasonEndTurn && strings.Contains(reply.Content().String(), "picked up")
	}, 10*time.Second, 10*time.Millisecond)
	require.Empty(t, c.limitRetries.pending)
}
