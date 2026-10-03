package cliagent

import (
	"context"
	"errors"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/stretchr/testify/require"
)

func TestExplicitUsageRefreshBypassesCacheAndKeepsLastGoodData(t *testing.T) {
	kind := catwalk.Type("test-usage-refresh")
	t.Cleanup(func() {
		delete(limitFetchers, kind)
		limitStore.Lock()
		defer limitStore.Unlock()
		delete(limitStore.byKind, kind)
		delete(limitStore.fetched, kind)
		delete(limitStore.pending, kind)
	})
	setLimits(kind, []Limit{{Name: "5h", Used: 10}})
	calls := 0
	limitFetchers[kind] = func(context.Context) ([]Limit, error) {
		calls++
		return []Limit{{Name: "5h", Used: 20}}, nil
	}
	require.False(t, RefreshLimits(t.Context(), kind))
	require.Zero(t, calls)
	require.NoError(t, RefreshLimitsNow(t.Context(), kind))
	require.Equal(t, 1, calls)
	require.Equal(t, float64(20), Limits(kind, "")[0].Used)
	limitFetchers[kind] = func(context.Context) ([]Limit, error) { return nil, errors.New("offline") }
	require.ErrorContains(t, RefreshLimitsNow(t.Context(), kind), "offline")
	require.Equal(t, float64(20), Limits(kind, "")[0].Used)
	limitFetchers[kind] = func(context.Context) ([]Limit, error) { return nil, nil }
	require.ErrorContains(t, RefreshLimitsNow(t.Context(), kind), "No usage windows")
	limitStore.Lock()
	limitStore.pending[kind] = true
	limitStore.Unlock()
	require.ErrorContains(t, RefreshLimitsNow(t.Context(), kind), "already in progress")
}
