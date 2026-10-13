// Package kittest holds test helpers shared by the dialog package and its
// subpackages. It is only imported from tests.
package kittest

import (
	"testing"

	"github.com/charmbracelet/crush/internal/ui/list"
	"github.com/sahilm/fuzzy"
	"github.com/stretchr/testify/require"
)

// VersionedItem is the cross-cutting interface every dialog list
// item must satisfy under F6: every documented mutator must bump
// the shared version counter so the list-level memo invalidates
// frozen entries.
type VersionedItem interface {
	list.Item
	Version() uint64
}

// RequireBump asserts that running mutate() advances the item's
// Version().
func RequireBump(t *testing.T, name string, item VersionedItem, mutate func()) {
	t.Helper()
	before := item.Version()
	mutate()
	after := item.Version()
	require.Greaterf(t, after, before, "%s must bump Version() (before=%d, after=%d)", name, before, after)
}

// RequireNoBump asserts that running mutate() leaves the item's
// Version() unchanged. Used to lock in the dedupe contract: a
// mutator called with a value identical to the current state must
// not gratuitously invalidate the list cache.
func RequireNoBump(t *testing.T, name string, item VersionedItem, mutate func()) {
	t.Helper()
	before := item.Version()
	mutate()
	after := item.Version()
	require.Equalf(t, before, after, "%s must NOT bump Version() when state is unchanged (before=%d, after=%d)", name, before, after)
}

// EquivMatch returns a fuzzy.Match whose fields and indexes are
// equivalent to the supplied seed but allocated as a fresh struct
// so callers exercise the value-equality dedupe path rather than
// referential equality.
func EquivMatch(seed fuzzy.Match) fuzzy.Match {
	return fuzzy.Match{
		Str:            seed.Str,
		Index:          seed.Index,
		Score:          seed.Score,
		MatchedIndexes: append([]int(nil), seed.MatchedIndexes...),
	}
}
