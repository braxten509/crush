package cliagent

import (
	"context"
	"testing"
	"time"

	"charm.land/catwalk/v3/pkg/catwalk"
	"github.com/stretchr/testify/require"
)

// A turn in one engine reads its sign-in's usage; another engine (the
// window's) takes that newer reading from the saved file without asking
// the tool, and never goes back to an older one (owner, 2026-11-12).
func TestSharedReadingNewerOneIsTaken(t *testing.T) {
	kind := catwalk.Type("adopt-saved-test")
	asked := 0
	defer SetLimitFetchersForTest(map[catwalk.Type]func(context.Context) ([]Limit, error){
		kind: func(context.Context) ([]Limit, error) {
			asked++
			return []Limit{{Name: "5h", Used: 1}}, nil
		},
	})()
	nextEngine := shareLimitsForTest(t, kind)
	k := limitKey{kind: kind}
	now := time.Now()

	// Another engine's turn read 55 used a minute ago; the window's engine
	// has its own 39 from two minutes ago.
	setLimitsAt(k, []Limit{{Name: "5h", Used: 55}}, -1, now.Add(-time.Minute))
	nextEngine()
	setLimitsAt(k, []Limit{{Name: "5h", Used: 39}}, -1, now.Add(-2*time.Minute))

	RefreshLimits(t.Context(), kind)
	require.InDelta(t, 55, Limits(kind, "")[0].Used, 0.001, "the newer reading is taken")
	require.Equal(t, 0, asked, "taking it asks nothing")

	writeLimitsFile(savedLimitsPath(k), sharedLimits{ReadAt: now.Add(-5 * time.Minute), Limits: []Limit{{Name: "5h", Used: 10}}})
	RefreshLimits(t.Context(), kind)
	require.InDelta(t, 55, Limits(kind, "")[0].Used, 0.001, "an older one is not")
}

// Two engines save readings in any order: the saved file keeps the newest
// one, and the newest per-model limits even from an older reading.
func TestSharedReadingSaveNeverGoesBack(t *testing.T) {
	kind := catwalk.Type("save-forward-test")
	shareLimitsForTest(t, kind)
	k := limitKey{kind: kind}
	now := time.Now()

	saveLimits(k, []Limit{{Name: "5h", Used: 55}}, 0, now, time.Time{})
	saveLimits(k, []Limit{{Name: "5h", Used: 39}, {Name: "Weekly Fable", Used: 70, Model: "Fable"}}, 0, now.Add(-time.Minute), now.Add(-time.Minute))

	r, ok := loadSharedLimits(savedLimitsPath(k))
	require.True(t, ok)
	require.True(t, r.ReadAt.Equal(now))
	require.Len(t, r.Limits, 2)
	require.InDelta(t, 55, r.Limits[0].Used, 0.001)
	require.InDelta(t, 70, r.Limits[1].Used, 0.001)
}
