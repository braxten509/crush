package cliagent

import (
	"context"
	"errors"
	"testing"

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
