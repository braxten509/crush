package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"
)

func TestCheckInLoopSendsUntilDone(t *testing.T) {
	t.Parallel()

	done := make(chan struct{})
	var mu sync.Mutex
	var sent []string
	send := func(_ context.Context, prompt string) {
		mu.Lock()
		defer mu.Unlock()
		sent = append(sent, prompt)
		if len(sent) == 3 {
			close(done)
		}
	}
	note := func(time.Duration) string { return "note" }
	checkInLoop(t.Context(), 5*time.Millisecond, done, func() bool { return false }, note, send)
	require.Equal(t, []string{"note", "note", "note"}, sent, "stops once the work is done")
}

func TestCheckInLoopWaitsForEachSendToFinish(t *testing.T) {
	t.Parallel()

	// A busy agent holds each send; the next wait starts only after it, so
	// check-ins never pile up.
	done := make(chan struct{})
	var starts []time.Time
	send := func(_ context.Context, _ string) {
		starts = append(starts, time.Now())
		time.Sleep(30 * time.Millisecond)
		if len(starts) == 2 {
			close(done)
		}
	}
	checkInLoop(t.Context(), 10*time.Millisecond, done, func() bool { return false }, func(time.Duration) string { return "" }, send)
	require.Len(t, starts, 2)
	require.GreaterOrEqual(t, starts[1].Sub(starts[0]), 40*time.Millisecond)
}

func TestCheckInLoopOffAndStops(t *testing.T) {
	t.Parallel()

	never := func(context.Context, string) { t.Fatal("must not send") }
	note := func(time.Duration) string { return "" }
	open := make(chan struct{})

	// Each call returning at all is the check: 0 turns check-ins off, then
	// done work, a closing Crush and an ended context each stop the loop.
	checkInLoop(t.Context(), 0, open, func() bool { return false }, note, never)

	closed := make(chan struct{})
	close(closed)
	checkInLoop(t.Context(), time.Millisecond, closed, func() bool { return false }, note, never)

	checkInLoop(t.Context(), time.Millisecond, open, func() bool { return true }, note, never)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	checkInLoop(ctx, time.Hour, open, func() bool { return false }, note, never)
}

// A check-in that fails (a cancelled turn, a provider error) doesn't end
// the session's check-ins: the work it reports on is still running.
func TestCheckInsKeepGoingAfterAFailedSend(t *testing.T) {
	calls := make(chan SessionAgentCall, 64)
	var failed sync.Once
	c := sessionRunTestCoordinator(t, func(ctx context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		calls <- call
		var err error
		failed.Do(func() { err = context.Canceled })
		if err != nil {
			return nil, err
		}
		return fakeSessionReply(call, "received"), nil
	})
	h := c.tasks
	h.checkInEvery = 200 * time.Millisecond
	session, err := h.c.sessions.Create(t.Context(), "failing")
	require.NoError(t, err)
	remove := h.addCheckIn(session.ID, fixedWork("Job A", "build", "a out", time.Now()))
	defer remove()
	for range 2 {
		select {
		case <-calls:
		case <-time.After(2 * time.Second):
			t.Fatal("check-ins stopped after a failed one")
		}
	}
}

func fixedWork(id, name, output string, started time.Time) checkInWork {
	return checkInWork{id: id, name: name, started: started, ctx: context.Background(), progress: func() string { return output }}
}

func TestCheckInNotificationIsARunningTaskResult(t *testing.T) {
	t.Parallel()

	now := time.Now()
	text := checkInNotification([]checkInWork{fixedWork("Job 004", "Background process: run tests", "  3/10 runs done  ", now.Add(-2*time.Minute))}, now)
	name, status, ok := ParseTaskNotification(text)
	require.True(t, ok)
	require.Equal(t, "Background process: run tests", name)
	require.Equal(t, string(TaskRunning), status)
	require.Contains(t, text, "<task-id>Job 004</task-id>")
	require.Contains(t, text, "[Job 004] Background process: run tests, running 2m0s")
	require.Contains(t, text, "Latest progress:\n3/10 runs done\n")

	require.Contains(t, checkInNotification([]checkInWork{fixedWork("t1", "review", " ", now)}, now), "(no output yet)")
	require.Empty(t, checkInNotification(nil, now), "no work, no message")
}

// Several running items share one message and its output budget.
func TestCheckInNotificationListsAllWorkWithinBudget(t *testing.T) {
	t.Parallel()

	now := time.Now()
	two := checkInNotification([]checkInWork{
		fixedWork("Job 001", "Background process: build", "compiling", now.Add(-time.Minute)),
		fixedWork("t2", "review", "half done", now),
	}, now)
	name, status, ok := ParseTaskNotification(two)
	require.True(t, ok)
	require.Equal(t, "2 background tasks", name)
	require.Equal(t, string(TaskRunning), status)
	require.NotContains(t, two, "<task-id>")
	require.Contains(t, two, "2 still running")
	require.Contains(t, two, "[Job 001] Background process: build, running 1m0s\nLatest progress:\ncompiling")
	require.Contains(t, two, "[t2] review, running 0s\nLatest progress:\nhalf done")

	var many []checkInWork
	for i := range 50 {
		many = append(many, fixedWork(fmt.Sprint(i), "job", strings.Repeat("output line\n", 400), now))
	}
	text := checkInNotification(many, now)
	require.Less(t, len(text), checkInTotalBytes+50*60, "fifty items stay within the shared budget plus headings")
	require.Equal(t, 50, strings.Count(text, "Latest progress:"))
}

func TestTailTextKeepsTheEnd(t *testing.T) {
	t.Parallel()

	var lines []string
	for i := range 40 {
		lines = append(lines, strings.Repeat("x", i%3)+"line")
	}
	lines[39] = "last line"
	tail := tailText(strings.Join(lines, "\n"), checkInTailLines, checkInTotalBytes)
	require.Len(t, strings.Split(tail, "\n"), checkInTailLines)
	require.True(t, strings.HasSuffix(tail, "last line"))

	long := tailText(strings.Repeat("y", 5000)+"end", checkInTailLines, 1500)
	require.True(t, strings.HasPrefix(long, "…"))
	require.True(t, strings.HasSuffix(long, "end"))
	require.LessOrEqual(t, len(long), 1500+len("…"))
}

// checkInTest is a hub with fast check-ins whose agent turns are recorded.
func checkInTest(t *testing.T) (*taskHub, chan SessionAgentCall) {
	t.Helper()
	calls := make(chan SessionAgentCall, 64)
	c := sessionRunTestCoordinator(t, func(ctx context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		calls <- call
		return fakeSessionReply(call, "received"), nil
	})
	c.tasks.checkInEvery = 200 * time.Millisecond
	return c.tasks, calls
}

// Two running items in one session get one combined check-in per round,
// another session gets its own, and check-ins stop once the work ends.
func TestSessionGetsOneCheckInForAllItsWork(t *testing.T) {
	h, calls := checkInTest(t)
	first, err := h.c.sessions.Create(t.Context(), "first")
	require.NoError(t, err)
	second, err := h.c.sessions.Create(t.Context(), "second")
	require.NoError(t, err)
	removeA := h.addCheckIn(first.ID, fixedWork("Job A", "build", "a out", time.Now()))
	removeB := h.addCheckIn(first.ID, fixedWork("Job B", "tests", "b out", time.Now()))
	removeC := h.addCheckIn(second.ID, fixedWork("Job C", "other", "c out", time.Now()))

	// Collect until each chat has had two rounds (the limit only matters
	// if check-ins stop).
	perSession := map[string]int{}
	deadline := time.After(2 * time.Second)
collect:
	for perSession[first.ID] < 2 || perSession[second.ID] < 2 {
		select {
		case call := <-calls:
			perSession[call.SessionID]++
			switch call.SessionID {
			case first.ID:
				require.Contains(t, call.Prompt, "[Job A]")
				require.Contains(t, call.Prompt, "[Job B]")
				require.NotContains(t, call.Prompt, "[Job C]")
			case second.ID:
				require.Contains(t, call.Prompt, "[Job C]")
				require.NotContains(t, call.Prompt, "[Job A]")
			}
		case <-deadline:
			break collect
		}
	}
	require.GreaterOrEqual(t, perSession[first.ID], 2)
	// One message per item would give the first chat (two items) twice the
	// second chat's count.
	require.LessOrEqual(t, perSession[first.ID], perSession[second.ID]+1, "one message per round, not one per item")
	require.GreaterOrEqual(t, perSession[second.ID], 2)

	removeA()
	select {
	case call := <-calls:
		if call.SessionID == first.ID {
			require.NotContains(t, call.Prompt, "[Job A]", "removed work is no longer listed")
		}
	case <-time.After(time.Second):
		t.Fatal("check-ins stopped while work was still running")
	}
	removeB()
	removeC()
	removeC() // removing twice is harmless
	// A check-in already on its way when the work ended may still land.
	time.Sleep(300 * time.Millisecond)
	for len(calls) > 0 {
		<-calls
	}
	require.Never(t, func() bool { return len(calls) > 0 }, 400*time.Millisecond, 20*time.Millisecond, "check-ins continue after all work ended")
	h.mu.Lock()
	defer h.mu.Unlock()
	require.Empty(t, h.checkIns)
}

// Real background jobs in one session share check-ins too.
func TestBackgroundJobsShareOneCheckIn(t *testing.T) {
	h, sessionID, notices := nativeJobTest(t)
	// Each start takes FastFailureWait (short in tests); the round leaves
	// room for both.
	h.checkInEvery = 400 * time.Millisecond
	hold, _ := gate(t)
	first := startNativeJob(t, h, sessionID, "printf one; "+hold)
	second := startNativeJob(t, h, sessionID, "printf two; "+hold)
	select {
	case notice := <-notices:
		require.Contains(t, notice, "<status>running</status>")
		require.Contains(t, notice, "[Job "+first.ID+"]")
		require.Contains(t, notice, "[Job "+second.ID+"]")
	case <-time.After(6 * time.Second):
		t.Fatal("check-in missing")
	}
}

func TestCheckInInstructionsFollowTheSetting(t *testing.T) {
	t.Parallel()

	require.Empty(t, (*taskHub)(nil).checkInInstructions())
}

func TestCheckInInstructionsNameTheInterval(t *testing.T) {
	t.Parallel()

	text := fmtCheckIn(3 * time.Minute)
	require.Contains(t, text, "every 3 minutes")
	require.Contains(t, text, "status running")
	require.Contains(t, fmtCheckIn(time.Minute), "every minute")
}

// While only services run, no note comes (owner, 2026-10-25: no "Still
// running" notice for servers); other work's notes name the services in one
// line, without their output.
func TestServiceOnlyCheckInsStayQuiet(t *testing.T) {
	h, calls := checkInTest(t)
	h.checkInEvery = 100 * time.Millisecond
	session, err := h.c.sessions.Create(t.Context(), "services")
	require.NoError(t, err)
	service := fixedWork("Job S", "server", "noisy log", time.Now())
	service.service = true
	removeService := h.addCheckIn(session.ID, service)
	defer removeService()

	// Three rounds pass with no note.
	quiet := func() {
		limit := time.After(350 * time.Millisecond)
		for {
			select {
			case call := <-calls:
				if strings.Contains(call.Prompt, "[Job A]") {
					continue // on its way before Job A ended
				}
				t.Fatalf("a note while only services run: %s", call.Prompt)
			case <-limit:
				return
			}
		}
	}
	quiet()

	removeJob := h.addCheckIn(session.ID, fixedWork("Job A", "build", "a out", time.Now()))
	select {
	case call := <-calls:
		require.Contains(t, call.Prompt, "[Job A] build")
		require.Contains(t, call.Prompt, "a out")
		require.Contains(t, call.Prompt, "1 service still running in the background: [Job S] server, up")
		require.NotContains(t, call.Prompt, "noisy log")
	case <-time.After(time.Second):
		t.Fatal("no check-in once other work started")
	}
	removeJob()
	quiet()
}

// Programs a chat left running outside crush bg get check-ins too (owner,
// 2026-10-20: "the 2 minute reminder never came"): a plain one as work,
// with how to stop it, and one on a local port as a service, named in other
// work's notes but never sent one alone; each leaves when a scan no longer finds it.
func TestStrayProcessesGetCheckIns(t *testing.T) {
	h, calls := checkInTest(t)
	h.checkInEvery = 100 * time.Millisecond
	session, err := h.c.sessions.Create(t.Context(), "strays")
	require.NoError(t, err)
	adb := Server{Port: 5037, Process: Process{PID: 101, Command: "adb -L tcp:5037 fork-server server", Started: time.Now(), Session: session.ID}}
	watcher := Process{PID: 202, Command: "python3 watch.py", Started: time.Now(), Session: session.ID}
	known := strayCheckIns{}
	h.syncStrays(known, []Server{adb}, []Process{watcher})
	h.syncStrays(known, []Server{adb}, []Process{watcher}) // a second scan adds nothing

	select {
	case call := <-calls:
		require.Equal(t, session.ID, call.SessionID)
		require.Contains(t, call.Prompt, "[PID 202] python3 watch.py")
		require.Contains(t, call.Prompt, "kill 202")
		require.Contains(t, call.Prompt, "1 service still running in the background: [PID 101] adb · 5037, up")
	case <-time.After(time.Second):
		t.Fatal("no check-in for a stray process")
	}

	// The server alone gets no note: three rounds pass quietly.
	h.syncStrays(known, []Server{adb}, nil)
	limit := time.After(350 * time.Millisecond)
collect:
	for {
		select {
		case call := <-calls:
			if strings.Contains(call.Prompt, "[PID 202]") {
				continue // on its way before the program ended
			}
			t.Fatalf("a note for a stray server alone: %s", call.Prompt)
		case <-limit:
			break collect
		}
	}

	h.syncStrays(known, nil, nil)
	require.Empty(t, known)
	h.mu.Lock()
	defer h.mu.Unlock()
	require.Empty(t, h.checkIns)
}

// A check-in turn is the parent's own: stopping the helper it reports on
// (or the helper ending) must not cut the parent's turn short. It once ran
// on the helper's context, so a parent that stopped its helper from that
// turn ended with "user cancelled" (owner, 2026-10-27).
func TestStoppingWorkLeavesTheCheckInTurnRunning(t *testing.T) {
	turns := make(chan context.Context, 8)
	release := make(chan struct{})
	c := sessionRunTestCoordinator(t, func(ctx context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		turns <- ctx
		<-release
		return fakeSessionReply(call, "received"), nil
	})
	defer close(release)
	h := c.tasks
	h.checkInEvery = 200 * time.Millisecond
	parent, err := c.sessions.Create(t.Context(), "parent")
	require.NoError(t, err)
	helperCtx, stopHelper := context.WithCancel(context.Background())
	work := fixedWork("t1", "helper", "working", time.Now())
	work.ctx = helperCtx
	remove := h.addCheckIn(parent.ID, work)
	defer remove()

	var turn context.Context
	select {
	case turn = <-turns:
	case <-time.After(2 * time.Second):
		t.Fatal("no check-in turn started")
	}
	stopHelper()
	require.Never(t, func() bool { return turn.Err() != nil }, 300*time.Millisecond, 10*time.Millisecond, "stopping the helper cut the parent's check-in turn short")
}
