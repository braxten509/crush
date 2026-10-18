package backend

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/memwatch"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/stretchr/testify/require"
)

// fakeMemory is a memory reader whose readings the test sets.
type fakeMemory struct {
	mu     sync.Mutex
	st     memwatch.Status
	checks int
}

func (f *fakeMemory) Check() memwatch.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks++
	return f.st
}

func (f *fakeMemory) Recent(time.Duration) memwatch.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.st
}

func (f *fakeMemory) set(st memwatch.Status) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.st = st
}

// useFakeMemory makes f the memory reader, with checks that never tick on
// their own, for the rest of the test.
func useFakeMemory(t *testing.T, st memwatch.Status) *fakeMemory {
	t.Helper()
	f := &fakeMemory{st: st}
	t.Cleanup(setMemorySource(f, time.Hour))
	return f
}

// liveTestBackend is a test backend whose context ends with the test, so
// its engine-wide state goes too.
func liveTestBackend(t *testing.T) *Backend {
	t.Helper()
	b, _ := newTestBackend(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	b.ctx = ctx
	return b
}

// engineEvents drains the engine events waiting on ch.
func engineEvents(ch <-chan any) []EngineEvent {
	var out []EngineEvent
	for {
		select {
		case ev := <-ch:
			out = append(out, ev.(EngineEvent))
		default:
			return out
		}
	}
}

// TestMemoryStatusRouteAndEvent: the route mirrors the memory reader, and
// a change sends one memory_status event, not one per check
// (engine-design.md 5.9 c).
func TestMemoryStatusRouteAndEvent(t *testing.T) {
	t.Setenv("CRUSH_GLOBAL_DATA", t.TempDir()) // no saved limit
	mem := useFakeMemory(t, memwatch.Status{Percent: 50})
	b := liveTestBackend(t)
	ws, _ := insertTestWorkspace(t, b, t.TempDir())
	ch, err := b.SubscribeClient(t.Context(), ws.ID, newClientID(t))
	require.NoError(t, err)

	got := b.MemoryStatus()
	require.Equal(t, 50, got.Percent)
	require.False(t, got.High)
	require.Empty(t, got.Top)
	require.Empty(t, got.Message)
	require.Equal(t, totalRAM() > 0, got.Supported)
	require.Equal(t, totalRAM(), got.TotalRAMBytes)
	require.Equal(t, totalRAM()/2, got.LimitBytes, "with no saved or configured limit, half the RAM")

	b.checkMemory()
	b.checkMemory()
	require.Empty(t, engineEvents(ch), "the first check is the baseline, and an unchanged one sends nothing")

	high := memwatch.Status{Percent: 91, High: true, Episode: 1, Top: []memwatch.Program{{Name: "big", Bytes: 3 << 30}}}
	mem.set(high)
	b.checkMemory()
	b.checkMemory()
	evs := engineEvents(ch)
	require.Len(t, evs, 1, "one event per change")
	require.Equal(t, pubsub.PayloadTypeMemoryStatus, evs[0].Type)
	sent := evs[0].Payload.(proto.MemoryStatus)
	require.True(t, sent.High)
	require.Equal(t, 91, sent.Percent)
	require.Equal(t, 1, sent.Episode)
	require.Equal(t, []proto.MemoryProgram{{Name: "big", Bytes: 3 << 30}}, sent.Top)
	require.Equal(t, high.Message(), sent.Message)

	got = b.MemoryStatus()
	// The box's use is read live each time.
	got.BoxUsedBytes, sent.BoxUsedBytes = 0, 0
	require.Equal(t, sent, got, "the route mirrors the reader")

	mem.set(memwatch.Status{Percent: 70, Episode: 1})
	b.checkMemory()
	evs = engineEvents(ch)
	require.Len(t, evs, 1)
	calm := evs[0].Payload.(proto.MemoryStatus)
	require.False(t, calm.High)
	require.Empty(t, calm.Top, "programs are listed only while high")
	require.Empty(t, calm.Message)
}
