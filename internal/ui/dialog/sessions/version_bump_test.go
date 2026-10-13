package sessions

import (
	"testing"

	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/dialog/internal/kittest"
	"github.com/charmbracelet/crush/internal/ui/list"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/sahilm/fuzzy"
)

// TestSessionItem_MutatorsBumpVersion covers F6 §4.5 for the
// sessions dialog items.
func TestSessionItem_MutatorsBumpVersion(t *testing.T) {
	t.Parallel()

	sty := styles.CharmtonePantera()
	item := &SessionItem{
		Versioned: list.NewVersioned(),
		Session:   session.Session{ID: "sess-1", Title: "My Session"},
		t:         &sty,
	}

	kittest.RequireBump(t, "SetFocused[true]", item, func() {
		item.SetFocused(true)
	})
	kittest.RequireNoBump(t, "SetFocused[true again]", item, func() {
		item.SetFocused(true)
	})

	match := fuzzy.Match{
		Str:            "My Session",
		Index:          0,
		Score:          5,
		MatchedIndexes: []int{0, 1, 2},
	}
	kittest.RequireBump(t, "SetMatch[new]", item, func() {
		item.SetMatch(match)
	})
	kittest.RequireNoBump(t, "SetMatch[same]", item, func() {
		item.SetMatch(kittest.EquivMatch(match))
	})
	kittest.RequireBump(t, "SetMatch[different]", item, func() {
		item.SetMatch(fuzzy.Match{
			Str:            "My Session",
			Index:          0,
			Score:          5,
			MatchedIndexes: []int{3, 4},
		})
	})
}
