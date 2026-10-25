package models

import (
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/ui/dialog/internal/kittest"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/sahilm/fuzzy"
)

// TestModelItem_MutatorsBumpVersion covers F6 §4.5 for the model
// picker items.
func TestModelItem_MutatorsBumpVersion(t *testing.T) {
	t.Parallel()

	sty := styles.CharmtonePantera()
	prov := catwalk.Provider{ID: "openai", Name: "OpenAI"}
	model := catwalk.Model{ID: "gpt-4", Name: "GPT-4"}
	item := NewModelItem(&sty, prov, model, ModelTypeChat, true)

	kittest.RequireBump(t, "SetFocused[true]", item, func() {
		item.SetFocused(true)
	})
	kittest.RequireNoBump(t, "SetFocused[true again]", item, func() {
		item.SetFocused(true)
	})

	match := fuzzy.Match{
		Str:            "GPT-4",
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
			Str:            "GPT-4",
			Index:          0,
			Score:          5,
			MatchedIndexes: []int{1},
		})
	})
}
