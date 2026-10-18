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

func TestEnsureFailureMarksStuckAfterThree(t *testing.T) {
	t.Cleanup(SetSSEBackoffForTest(time.Millisecond, 5*time.Millisecond))

	// An engine connection whose engine is gone: its socket file is missing.
	dir, err := os.MkdirTemp("", "ce")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	c, err := client.NewClient(dir, "unix", filepath.Join(dir, "engine.sock"))
	require.NoError(t, err)
	w := NewClientWorkspace(c, proto.Workspace{ID: uuid.NewString(), Path: dir})

	// Two failures, a success that does not bring the engine back, then
	// failures: only the third failure in a row is stuck.
	errNoStart := errors.New("engine did not start")
	results := []error{errNoStart, errNoStart, nil, errNoStart, errNoStart, errNoStart}
	var calls atomic.Int32
	var limited atomic.Bool
	limited.Store(true)
	w.SetEnsureEngine(func(ctx context.Context) error {
		n := int(calls.Add(1))
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > ensureEngineTimeout {
			limited.Store(false)
		}
		if n <= len(results) && results[n-1] == nil {
			return nil
		}
		return fmt.Errorf("attempt %d: %w", n, errNoStart)
	})

	events := make(chan ConnectionEvent, 64)
	send := func(msg tea.Msg) {
		if ev, ok := msg.(ConnectionEvent); ok {
			select {
			case events <- ev:
			default:
			}
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
			t.Fatalf("never reported stuck; events: %+v, ensures: %d", got, calls.Load())
		}
	}

	require.Equal(t, ConnectionDegraded, got[0].State)
	require.False(t, got[0].Stuck, "the first failure is only degraded")
	require.True(t, client.IsUnreachable(got[0].Err), "the first event reports the dead engine: %v", got[0].Err)
	for _, ev := range got[:len(got)-1] {
		require.False(t, ev.Stuck, "stuck before three failures in a row: %+v", ev)
	}
	stuck := got[len(got)-1]
	require.Equal(t, ConnectionDegraded, stuck.State)
	require.ErrorIs(t, stuck.Err, errNoStart, "the stuck event carries the ensure error")
	require.ErrorContains(t, stuck.Err, "attempt 6", "the success reset the count")
	require.True(t, limited.Load(), "each ensure runs with the 15 s limit")
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
