package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCheckInLoopSendsUntilDone(t *testing.T) {
	t.Parallel()

	done := make(chan struct{})
	var mu sync.Mutex
	var sent []string
	send := func(_ context.Context, prompt string) error {
		mu.Lock()
		defer mu.Unlock()
		sent = append(sent, prompt)
		if len(sent) == 3 {
			close(done)
		}
		return nil
	}
	note := func(time.Duration) string { return "note" }
	err := checkInLoop(t.Context(), 5*time.Millisecond, done, func() bool { return false }, note, send)
	require.NoError(t, err)
	require.Equal(t, []string{"note", "note", "note"}, sent, "stops once the work is done")
}

func TestCheckInLoopWaitsForEachSendToFinish(t *testing.T) {
	t.Parallel()

	// A busy agent holds each send; the next wait starts only after it, so
	// check-ins never pile up.
	done := make(chan struct{})
	var starts []time.Time
	send := func(_ context.Context, _ string) error {
		starts = append(starts, time.Now())
		time.Sleep(30 * time.Millisecond)
		if len(starts) == 2 {
			close(done)
		}
		return nil
	}
	err := checkInLoop(t.Context(), 10*time.Millisecond, done, func() bool { return false }, func(time.Duration) string { return "" }, send)
	require.NoError(t, err)
	require.Len(t, starts, 2)
	require.GreaterOrEqual(t, starts[1].Sub(starts[0]), 40*time.Millisecond)
}

func TestCheckInLoopOffAndStops(t *testing.T) {
	t.Parallel()

	never := func(context.Context, string) error { t.Fatal("must not send"); return nil }
	note := func(time.Duration) string { return "" }
	open := make(chan struct{})

	require.NoError(t, checkInLoop(t.Context(), 0, open, func() bool { return false }, note, never), "0 turns check-ins off")

	closed := make(chan struct{})
	close(closed)
	require.NoError(t, checkInLoop(t.Context(), time.Millisecond, closed, func() bool { return false }, note, never))

	require.NoError(t, checkInLoop(t.Context(), time.Millisecond, open, func() bool { return true }, note, never), "Crush closing")

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.NoError(t, checkInLoop(ctx, time.Hour, open, func() bool { return false }, note, never))

	failing := func(context.Context, string) error { return errors.New("session gone") }
	require.EqualError(t, checkInLoop(t.Context(), time.Millisecond, open, func() bool { return false }, note, failing), "session gone")
}

func TestCheckInNotificationIsARunningTaskResult(t *testing.T) {
	t.Parallel()

	text := checkInNotification("004", "Background process: run tests", 2*time.Minute, "  3/10 runs done  ")
	name, status, ok := ParseTaskNotification(text)
	require.True(t, ok)
	require.Equal(t, "Background process: run tests", name)
	require.Equal(t, string(TaskRunning), status)
	require.Contains(t, text, "still running after 2m0s")
	require.Contains(t, text, "Latest progress:\n3/10 runs done\n")

	require.Contains(t, checkInNotification("t1", "review", time.Minute, " "), "(no output yet)")
}

func TestTailTextKeepsTheEnd(t *testing.T) {
	t.Parallel()

	var lines []string
	for i := range 40 {
		lines = append(lines, strings.Repeat("x", i%3)+"line")
	}
	lines[39] = "last line"
	tail := tailText(strings.Join(lines, "\n"))
	require.Len(t, strings.Split(tail, "\n"), checkInTailLines)
	require.True(t, strings.HasSuffix(tail, "last line"))

	long := tailText(strings.Repeat("y", 5000) + "end")
	require.True(t, strings.HasPrefix(long, "…"))
	require.True(t, strings.HasSuffix(long, "end"))
	require.LessOrEqual(t, len(long), checkInTailBytes+len("…"))
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
