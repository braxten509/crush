package list

import (
	"slices"
	"strings"

	"github.com/sahilm/fuzzy"
)

// FilterableItem is an item that can be filtered via a query.
type FilterableItem interface {
	Item
	// Filter returns the value to be used for filtering.
	Filter() string
}

// MatchSettable is an interface for items that can have their match indexes
// and match score set.
type MatchSettable interface {
	SetMatch(fuzzy.Match)
}

// FilterableList is a list that takes filterable items that can be filtered
// via a settable query.
type FilterableList struct {
	*List
	items []FilterableItem
	query string
	// literal makes a query match whole typed words, not scattered letters.
	literal bool
}

// SetLiteral makes the query match its words as typed, letters side by
// side and case folded, keeping the items' own order (the chat search,
// owner 2026-11-16) instead of fuzzy-matching letters anywhere.
func (f *FilterableList) SetLiteral(literal bool) {
	f.literal = literal
}

// NewFilterableList creates a new filterable list.
func NewFilterableList(items ...FilterableItem) *FilterableList {
	f := &FilterableList{
		List:  NewList(),
		items: items,
	}
	f.RegisterRenderCallback(FocusedRenderCallback(f.List))
	f.SetItems(items...)
	return f
}

// SetItems sets the list items and updates the filtered items.
func (f *FilterableList) SetItems(items ...FilterableItem) {
	f.items = items
	fitems := make([]Item, len(items))
	for i, item := range items {
		fitems[i] = item
	}
	f.List.SetItems(fitems...)
}

// AppendItems appends items to the list and updates the filtered items.
func (f *FilterableList) AppendItems(items ...FilterableItem) {
	f.items = append(f.items, items...)
	itms := make([]Item, len(f.items))
	for i, item := range f.items {
		itms[i] = item
	}
	f.List.SetItems(itms...)
}

// PrependItems prepends items to the list and updates the filtered items.
func (f *FilterableList) PrependItems(items ...FilterableItem) {
	f.items = append(items, f.items...)
	itms := make([]Item, len(f.items))
	for i, item := range f.items {
		itms[i] = item
	}
	f.List.SetItems(itms...)
}

// SetFilter sets the filter query and updates the list items.
func (f *FilterableList) SetFilter(q string) {
	f.query = q
	f.List.SetItems(f.FilteredItems()...)
	f.ScrollToTop()
}

// FilterableItemsSource is a type that implements [fuzzy.Source] for filtering
// [FilterableItem]s.
type FilterableItemsSource []FilterableItem

// Len returns the length of the source.
func (f FilterableItemsSource) Len() int {
	return len(f)
}

// String returns the string representation of the item at index i.
func (f FilterableItemsSource) String(i int) string {
	return f[i].Filter()
}

// Items returns every item of the list, including the ones the current
// query filters out.
func (f *FilterableList) Items() []FilterableItem {
	return f.items
}

// FilteredItems returns the visible items after filtering.
func (f *FilterableList) FilteredItems() []Item {
	if f.query == "" {
		items := make([]Item, len(f.items))
		for i, item := range f.items {
			if ms, ok := item.(MatchSettable); ok {
				ms.SetMatch(fuzzy.Match{})
				item = ms.(FilterableItem)
			}
			items[i] = item
		}
		return items
	}

	items := FilterableItemsSource(f.items)
	var matches fuzzy.Matches
	if f.literal {
		matches = literalFind(f.query, items)
	} else {
		matches = fuzzy.FindFrom(f.query, items)
	}
	matchedItems := []Item{}
	resultSize := len(matches)
	for i := range resultSize {
		match := matches[i]
		item := items[match.Index]
		if ms, ok := item.(MatchSettable); ok {
			ms.SetMatch(match)
			item = ms.(FilterableItem)
		}
		matchedItems = append(matchedItems, item)
	}

	return matchedItems
}

// Render renders the filterable list.
func (f *FilterableList) Render() string {
	f.List.SetItems(f.FilteredItems()...)
	return f.List.Render()
}

// literalFind keeps the items whose text holds every word of query, case
// folded, in their own order. MatchedIndexes are the byte offsets of every
// letter of every place a word appears, for underlining.
func literalFind(query string, items FilterableItemsSource) fuzzy.Matches {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return nil
	}
	var matches fuzzy.Matches
	for i := range items {
		text := items.String(i)
		lower := strings.ToLower(text)
		if len(lower) != len(text) {
			// Folding changed the byte length: offsets would not line up.
			lower = text
		}
		var marks []int
		all := true
		for _, word := range words {
			at := strings.Index(lower, word)
			if at < 0 {
				all = false
				break
			}
			for at >= 0 {
				for b := at; b < at+len(word); b++ {
					marks = append(marks, b)
				}
				next := strings.Index(lower[at+len(word):], word)
				if next < 0 {
					break
				}
				at += len(word) + next
			}
		}
		if !all {
			continue
		}
		slices.Sort(marks)
		matches = append(matches, fuzzy.Match{Str: text, Index: i, MatchedIndexes: slices.Compact(marks)})
	}
	return matches
}
