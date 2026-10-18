package backend

import (
	"context"
	"sync"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/memwatch"
	"github.com/stretchr/testify/require"
)

// countingFetchers are usage fetchers that count their calls.
type countingFetchers struct {
	mu    sync.Mutex
	calls map[catwalk.Type]int
}

func (c *countingFetchers) fetcher(kind catwalk.Type) func(context.Context) ([]cliagent.Limit, error) {
	return func(context.Context) ([]cliagent.Limit, error) {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.calls[kind]++
		return []cliagent.Limit{{Name: "5h", Used: 10}}, nil
	}
}

func (c *countingFetchers) count(kind catwalk.Type) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[kind]
}

// TestEngineRefreshesLimitsAtStart: the engine's first workspace forces one
// refresh per AI tool, so the bars do not wait for a turn; later
// workspaces and apps add none (engine-design.md 5.9 e).
func TestEngineRefreshesLimitsAtStart(t *testing.T) {
	useFakeMemory(t, memwatch.Status{})
	const kindA, kindB = catwalk.Type("fake-cli-a"), catwalk.Type("fake-cli-b")
	fetchers := &countingFetchers{calls: map[catwalk.Type]int{}}
	t.Cleanup(cliagent.SetLimitFetchersForTest(map[catwalk.Type]func(context.Context) ([]cliagent.Limit, error){
		kindA: fetchers.fetcher(kindA),
		kindB: fetchers.fetcher(kindB),
	}))

	// A fresh reading for kindA: only a forced refresh asks again.
	require.NoError(t, cliagent.RefreshLimitsNow(t.Context(), kindA))
	require.False(t, cliagent.RefreshLimits(t.Context(), kindA), "a recent reading holds off an unforced refresh")
	require.Equal(t, 1, fetchers.count(kindA))

	b := liveTestBackend(t)
	ws, _ := insertTestWorkspace(t, b, t.TempDir())
	require.NoError(t, RegisterClientForTesting(b, ws, newClientID(t)))
	require.Eventually(t, func() bool {
		return fetchers.count(kindA) == 2 && fetchers.count(kindB) == 1
	}, 5*time.Second, 5*time.Millisecond, "one forced refresh per kind at the first workspace")

	require.NoError(t, RegisterClientForTesting(b, ws, newClientID(t)))
	other, _ := insertTestWorkspace(t, b, t.TempDir())
	require.NoError(t, RegisterClientForTesting(b, other, newClientID(t)))
	time.Sleep(100 * time.Millisecond)
	require.Equal(t, 2, fetchers.count(kindA), "later apps and workspaces add no refresh")
	require.Equal(t, 1, fetchers.count(kindB))

	for _, k := range CurrentLimits().CLIs {
		require.Equal(t, "ready", k.State, k.Kind)
		require.False(t, k.ReadAt.IsZero(), k.Kind)
	}
	drainBackend(t, b)
}
