// Package kittest holds test helpers shared by the dialog package and its
// subpackages. It is only imported from tests.
package kittest

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/crush/internal/ui/list"
	uv "github.com/charmbracelet/ultraviolet"
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

// ScreenText flattens the screen buffer into newline-separated rows.
func ScreenText(scr uv.ScreenBuffer) string {
	bounds := scr.Bounds()
	rows := make([]string, bounds.Dy())
	for y := range bounds.Dy() {
		var b strings.Builder
		for x := range bounds.Dx() {
			if cell := scr.CellAt(x, y); cell != nil {
				b.WriteString(cell.Content)
			}
		}
		rows[y] = b.String()
	}
	return strings.Join(rows, "\n")
}

// KeyMsg returns the key press message for typing r.
func KeyMsg(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}
