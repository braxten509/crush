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

	perSession := map[string]int{}
	deadline := time.After(time.Second)
collect:
	for {
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
	require.LessOrEqual(t, perSession[first.ID], 6, "one message per round, not one per item")
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
	require.Never(t, func() bool { return len(calls) > 0 }, 700*time.Millisecond, 20*time.Millisecond, "check-ins continue after all work ended")
	h.mu.Lock()
	defer h.mu.Unlock()
	require.Empty(t, h.checkIns)
}

// Real background jobs in one session share check-ins too.
func TestBackgroundJobsShareOneCheckIn(t *testing.T) {
	h, sessionID, notices := nativeJobTest(t)
	// Each start takes about a second; the round leaves room for both on a
	// busy machine.
	h.checkInEvery = 4 * time.Second
	first := startNativeJob(t, h, sessionID, "printf one; sleep 10")
	second := startNativeJob(t, h, sessionID, "printf two; sleep 10")
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
