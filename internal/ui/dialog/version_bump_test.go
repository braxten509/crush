package dialog

import (
	"testing"

	"github.com/charmbracelet/crush/internal/ui/dialog/internal/kittest"
	"github.com/charmbracelet/crush/internal/ui/list"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/sahilm/fuzzy"
)

// TestCommandItem_MutatorsBumpVersion covers F6 §4.5 for the
// commands palette items: SetFocused and SetMatch bump Version()
// on observable change and dedupe otherwise.
func TestCommandItem_MutatorsBumpVersion(t *testing.T) {
	t.Parallel()

	sty := styles.CharmtonePantera()
	item := NewCommandItem(&sty, "id", "Title", "ctrl+t", nil)

	kittest.RequireBump(t, "SetFocused[true]", item, func() {
		item.SetFocused(true)
	})
	kittest.RequireNoBump(t, "SetFocused[true again]", item, func() {
		item.SetFocused(true)
	})
	kittest.RequireBump(t, "SetFocused[false]", item, func() {
		item.SetFocused(false)
	})

	match := fuzzy.Match{
		Str:            "Title",
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
			Str:            "Title",
			Index:          0,
			Score:          5,
			MatchedIndexes: []int{0, 2},
		})
	})
}

// TestReasoningItem_MutatorsBumpVersion covers F6 §4.5 for the
// reasoning effort dialog items.
func TestReasoningItem_MutatorsBumpVersion(t *testing.T) {
	t.Parallel()

	sty := styles.CharmtonePantera()
	item := &ReasoningItem{
		Versioned: list.NewVersioned(),
		effort:    "medium",
		title:     "Medium",
		t:         &sty,
	}

	kittest.RequireBump(t, "SetFocused[true]", item, func() {
		item.SetFocused(true)
	})
	kittest.RequireNoBump(t, "SetFocused[true again]", item, func() {
		item.SetFocused(true)
	})

	match := fuzzy.Match{
		Str:            "Medium",
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
			Str:            "Medium",
			Index:          0,
			Score:          5,
			MatchedIndexes: []int{2, 3},
		})
	})
}
