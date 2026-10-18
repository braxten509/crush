package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/client"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// deadEngineWorkspace is an engine connection whose engine is gone: its
// socket file is missing.
func deadEngineWorkspace(t *testing.T) *ClientWorkspace {
	t.Helper()
	dir, err := os.MkdirTemp("", "ce")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	c, err := client.NewClient(dir, "unix", filepath.Join(dir, "engine.sock"))
	require.NoError(t, err)
	return NewClientWorkspace(c, proto.Workspace{ID: uuid.NewString(), Path: dir})
}

// runUntilStuck runs w's subscription loop until it reports the connection
// stuck, and returns the connection events it sent. onStuck, if set, runs
// in the loop as the stuck event is sent.
func runUntilStuck(t *testing.T, w *ClientWorkspace, onStuck func()) []ConnectionEvent {
	t.Helper()
	events := make(chan ConnectionEvent, 64)
	var once sync.Once
	send := func(msg tea.Msg) {
		ev, ok := msg.(ConnectionEvent)
		if !ok {
			return
		}
		if ev.Stuck && onStuck != nil {
			once.Do(onStuck)
		}
		select {
		case events <- ev:
		default:
		}
	}
	var wg sync.WaitGroup
	wg.Go(func() { w.runSubscription(send) })
	t.Cleanup(func() {
		w.subCancel()
		wg.Wait()
	})

	var got []ConnectionEvent
	timeout := time.After(10 * time.Second)
	for len(got) == 0 || !got[len(got)-1].Stuck {
		select {
		case ev := <-events:
			got = append(got, ev)
		case <-timeout:
			t.Fatalf("never reported stuck; events: %+v", got)
		}
	}
	return got
}

func TestEnsureFailureMarksStuckAfterThree(t *testing.T) {
	t.Cleanup(SetSSEBackoffForTest(time.Millisecond, 5*time.Millisecond))
	w := deadEngineWorkspace(t)

	// A failure, a success whose engine is gone at once, and a failure:
	// three failed ensures in a row, so the third is stuck.
	errNoStart := errors.New("engine did not start")
	var calls atomic.Int32
	var limited atomic.Bool
	limited.Store(true)
	w.SetEnsureEngine(func(ctx context.Context) error {
		n := int(calls.Add(1))
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > ensureEngineTimeout {
			limited.Store(false)
		}
		if n == 2 {
			return nil
		}
		return fmt.Errorf("attempt %d: %w", n, errNoStart)
	})

	got := runUntilStuck(t, w, nil)
	require.Equal(t, ConnectionDegraded, got[0].State)
	require.False(t, got[0].Stuck, "the first failure is only degraded")
	require.True(t, client.IsUnreachable(got[0].Err), "the first event reports the dead engine: %v", got[0].Err)
	for _, ev := range got[:len(got)-1] {
		require.False(t, ev.Stuck, "stuck before three failures in a row: %+v", ev)
	}
	stuck := got[len(got)-1]
	require.Equal(t, ConnectionDegraded, stuck.State)
	require.ErrorIs(t, stuck.Err, errNoStart, "the stuck event carries the ensure error")
	require.ErrorContains(t, stuck.Err, "attempt 3", "the success with a dead engine counted")
	require.True(t, limited.Load(), "each ensure runs with the 15 s limit")
}

// TestEngineDyingAtOnceMarksStuck: an ensure that always succeeds while
// every subscribe after it finds no engine ends stuck after three rounds,
// and the rounds wait the backoff instead of spinning.
func TestEngineDyingAtOnceMarksStuck(t *testing.T) {
	const initial = 40 * time.Millisecond
	t.Cleanup(SetSSEBackoffForTest(initial, time.Second))
	w := deadEngineWorkspace(t)

	var mu sync.Mutex
	var ensures []time.Time
	w.SetEnsureEngine(func(context.Context) error {
		mu.Lock()
		ensures = append(ensures, time.Now())
		mu.Unlock()
		return nil // reports success, but no engine ever answers
	})

	ensuresAtStuck := 0
	got := runUntilStuck(t, w, func() {
		mu.Lock()
		ensuresAtStuck = len(ensures)
		mu.Unlock()
	})
	stuck := got[len(got)-1]
	require.True(t, client.IsUnreachable(stuck.Err), "the stuck event reports the dead engine: %v", stuck.Err)
	require.Equal(t, 3, ensuresAtStuck, "stuck after three rounds")
	mu.Lock()
	defer mu.Unlock()
	require.GreaterOrEqual(t, ensures[1].Sub(ensures[0]), initial, "the second round waited the backoff")
	require.GreaterOrEqual(t, ensures[2].Sub(ensures[1]), 2*initial, "the third round waited the doubled backoff")
}

func TestPromptPendingIsHandledQuietly(t *testing.T) {
	t.Parallel()

	w := &ClientWorkspace{}
	msg, ok := w.translateLifecycleEvent(pubsub.Event[proto.PromptPending]{
		Type:    pubsub.UpdatedEvent,
		Payload: proto.PromptPending{SessionID: "chat", Kind: "permission", Pending: true},
	})
	require.True(t, ok)
	require.Nil(t, msg)
	_, ok = w.translateLifecycleEvent("something else")
	require.False(t, ok)
}

func TestRecreateArgsCarryProtocol(t *testing.T) {
	t.Parallel()

	w := &ClientWorkspace{ws: proto.Workspace{ID: "old", Path: "/p", DataDir: "/p/.crush", YOLO: true}}
	args := w.recreateArgs()
	require.Equal(t, proto.ProtocolVersion, args.Protocol)
	require.Empty(t, args.ID)
	require.Equal(t, "/p", args.Path)
}
