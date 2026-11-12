package cliagent

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"charm.land/catwalk/v3/pkg/catwalk"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// shareLimitsForTest shares readings in a folder of the test's own and
// returns a function that makes this process the next engine: another
// owner, with nothing read yet for kind.
func shareLimitsForTest(t *testing.T, kind catwalk.Type) (nextEngine func()) {
	t.Helper()
	dir := t.TempDir()
	savedDir, savedOwner := sharedLimitsDir, sharedLimitsOwner
	sharedLimitsDir = func() string { return dir }
	t.Cleanup(func() { sharedLimitsDir, sharedLimitsOwner = savedDir, savedOwner })
	return func() {
		sharedLimitsOwner = uuid.NewString()
		limitStore.Lock()
		forgetKind(kind)
		limitStore.Unlock()
	}
}

// Every open folder's engine reads the same sign-in (owner, 2026-11-12:
// five engines starting together were refused by Anthropic). The second
// engine takes the first one's reading rather than ask again; a read that
// must be the tool's own (a turn's usage) still asks.
func TestEnginesShareOneUsageReading(t *testing.T) {
	kind := catwalk.Type("share-reading-test")
	asked := 0
	defer SetLimitFetchersForTest(map[catwalk.Type]func(context.Context) ([]Limit, error){
		kind: func(context.Context) ([]Limit, error) {
			asked++
			return []Limit{{Name: "5h", Used: float64(10 * asked)}}, nil
		},
	})()
	nextEngine := shareLimitsForTest(t, kind)

	require.NoError(t, RefreshLimitsNow(t.Context(), kind))
	require.Equal(t, 1, asked)

	nextEngine()
	require.NoError(t, RefreshLimitsNow(t.Context(), kind))
	require.Equal(t, 1, asked, "the second engine takes the first one's reading")
	require.InDelta(t, 10, Limits(kind, "")[0].Used, 0.001)

	_, err := RefreshAccountLimits(freshLimits(t.Context()), kind, "", true)
	require.NoError(t, err)
	require.Equal(t, 2, asked, "a turn's own usage is asked for")
	require.InDelta(t, 20, Limits(kind, "")[0].Used, 0.001)
}

// A refusal is shared too: the next engine does not ask right after it.
func TestEnginesShareAUsageRefusal(t *testing.T) {
	kind := catwalk.Type("share-refusal-test")
	asked := 0
	defer SetLimitFetchersForTest(map[catwalk.Type]func(context.Context) ([]Limit, error){
		kind: func(context.Context) ([]Limit, error) {
			asked++
			return nil, errors.New("429 Too Many Requests")
		},
	})()
	nextEngine := shareLimitsForTest(t, kind)

	require.ErrorContains(t, RefreshLimitsNow(t.Context(), kind), "429")
	nextEngine()
	require.ErrorContains(t, RefreshLimitsNow(t.Context(), kind), "429")
	require.Equal(t, 1, asked, "the second engine does not ask again")
}

// An engine starts with nothing read. A refused first read used to leave
// the sign-in at "can't read" (owner, 2026-11-12): it now shows the last
// amounts any engine read, until a reading of its own arrives.
func TestRestartedEngineShowsTheLastAmounts(t *testing.T) {
	kind := catwalk.Type("remembered-reading-test")
	refuse := false
	defer SetLimitFetchersForTest(map[catwalk.Type]func(context.Context) ([]Limit, error){
		kind: func(context.Context) ([]Limit, error) {
			if refuse {
				return nil, errors.New("429 Too Many Requests")
			}
			return []Limit{{Name: "5h", Used: 40}}, nil
		},
	})()
	nextEngine := shareLimitsForTest(t, kind)
	require.NoError(t, RefreshLimitsNow(t.Context(), kind))

	// A restart long after: the shared reading is gone, the tool refuses.
	nextEngine()
	require.NoError(t, os.Remove(sharedLimitsPath(limitKey{kind: kind})))
	refuse = true
	require.ErrorContains(t, RefreshLimitsNow(t.Context(), kind), "429")
	require.Len(t, Limits(kind, ""), 1, "the remembered amounts stay")
	require.InDelta(t, 40, Limits(kind, "")[0].Used, 0.001)
}

// A tool's sign-ins are asked one after another with a gap, never all at
// once (owner, 2026-11-12).
func TestSignInsOfOneToolAreAskedWithAGap(t *testing.T) {
	kind := catwalk.Type("sign-in-gap-test")
	var mu sync.Mutex
	var asked []time.Time
	defer SetLimitFetchersForTest(map[catwalk.Type]func(context.Context) ([]Limit, error){
		kind: func(context.Context) ([]Limit, error) {
			mu.Lock()
			defer mu.Unlock()
			asked = append(asked, time.Now())
			return []Limit{{Name: "5h", Used: 1}}, nil
		},
	})()
	shareLimitsForTest(t, kind)
	saved := sharedLimitsGap
	sharedLimitsGap = 200 * time.Millisecond
	t.Cleanup(func() { sharedLimitsGap = saved })

	var wg sync.WaitGroup
	for _, account := range []string{"", "second"} {
		wg.Go(func() { _, _ = RefreshAccountLimits(t.Context(), kind, account, true) })
	}
	wg.Wait()
	require.Len(t, asked, 2)
	require.GreaterOrEqual(t, asked[1].Sub(asked[0]).Abs(), sharedLimitsGap)
}

// After a "too many asks" refusal no engine asks that sign-in again for a
// while, not even for a turn's own usage: asking on keeps the tool shut.
func TestTooManyAsksRefusalIsNotAskedAgain(t *testing.T) {
	kind := catwalk.Type("refusal-wait-test")
	asked := 0
	defer SetLimitFetchersForTest(map[catwalk.Type]func(context.Context) ([]Limit, error){
		kind: func(context.Context) ([]Limit, error) {
			asked++
			return nil, refusedError{msg: "429 Too Many Requests"}
		},
	})()
	shareLimitsForTest(t, kind)

	require.ErrorContains(t, RefreshLimitsNow(t.Context(), kind), "429")
	_, err := RefreshAccountLimits(freshLimits(t.Context()), kind, "", true)
	require.ErrorContains(t, err, "429")
	require.Equal(t, 1, asked, "the refused sign-in is left alone")
}

// A working chat reads its whole usage every few minutes for its model
// family's limit. All engines together ask that often, not each: the next
// engine takes the family's limit from the first one's reading and keeps
// its own turn's newer account windows.
func TestWorkingChatsOfTwoEnginesAskOnce(t *testing.T) {
	kind := catwalk.Type("working-chat-ask-test")
	asked := 0
	defer SetLimitFetchersForTest(map[catwalk.Type]func(context.Context) ([]Limit, error){
		kind: func(context.Context) ([]Limit, error) {
			asked++
			return []Limit{{Name: "5h", Used: 10}, {Name: "Weekly Fable", Used: 70, Model: "Fable"}}, nil
		},
	})()
	nextEngine := shareLimitsForTest(t, kind)
	require.NoError(t, RefreshLimitsNow(t.Context(), kind))

	nextEngine()
	k := limitKey{kind: kind}
	setLimits(k, []Limit{{Name: "5h", Used: 50}}, -1) // the turn's own update
	refreshUsageDuringTurn(t.Context(), k)
	require.Equal(t, 1, asked, "the second engine does not ask again")
	limits := Limits(kind, "Fable")
	require.Len(t, limits, 2)
	require.InDelta(t, 50, limits[0].Used, 0.001)
	require.InDelta(t, 70, limits[1].Used, 0.001)
}
