package list

import (
	"strings"
)

// getItem renders (if needed) and returns the item at the given index.
// The result is served from the F6 cache when possible — see
// renderItemEntry for the cache-key semantics.
func (l *List) getItem(idx int) renderedItem {
	if idx < 0 || idx >= len(l.items) {
		return renderedItem{}
	}
	entry := l.renderItemEntry(idx)
	if entry == nil {
		return renderedItem{}
	}
	return renderedItem{content: entry.content, height: entry.height}
}

// renderItemEntry returns the cache entry for the given index, populating
// the cache on miss. The result must not be retained past the next
// invalidation (SetSize width change, SetItems, etc.).
//
// Render callbacks always run, even for frozen entries: callbacks
// are how the list discovers per-frame state changes (selection,
// highlight range) and they bump the item's version when those
// changes affect the rendered output. A frozen item whose callback
// run is a no-op (same focus, same highlight) keeps its stored
// version and the cache hit is preserved on the post-callback
// version check.
func (l *List) renderItemEntry(idx int) *listCacheEntry {
	if idx < 0 || idx >= len(l.items) {
		return nil
	}

	rawItem := l.items[idx]
	entry := l.cache[rawItem]

	// Run render callbacks. Callbacks may mutate the item (focus,
	// highlight) which in turn bumps its version when state actually
	// changes. We capture the post-callback version below.
	item := rawItem
	if len(l.renderCallbacks) > 0 {
		for _, cb := range l.renderCallbacks {
			if it := cb(idx, l.selectedIdx, item); it != nil {
				item = it
			}
		}
	}

	version := rawItem.Version()
	if entry != nil && entry.width == l.width && entry.version == version {
		// Cache hit — frozen or unfrozen, the entry content is
		// still correct because no version bump landed since the
		// last render. Selection-drag suppression turns this into
		// a miss only if the entry is frozen.
		if !entry.frozen {
			return entry
		}
		if _, suppressed := l.freezeSuppressed[rawItem]; !suppressed {
			return entry
		}
	}

	rendered := item.Render(l.width)
	rendered = strings.TrimRight(rendered, "\n")
	lines := strings.Split(rendered, "\n")
	height := len(lines)

	// Re-read the version after Render so that any version bumps
	// caused by Render itself (e.g. an item that mutates internal
	// state during rendering) are captured. Without this we would
	// freeze a stale entry under the post-render version.
	finalVersion := rawItem.Version()

	frozen := false
	if rawItem.Finished() {
		if _, suppressed := l.freezeSuppressed[rawItem]; !suppressed {
			frozen = true
		}
	}

	if entry == nil {
		entry = &listCacheEntry{}
		l.cache[rawItem] = entry
	}
	// If the item's rendered height changed, the cached total height is
	// no longer valid and must be recomputed on the next TotalHeight call.
	if entry.height != height {
		l.totalHeightValid = false
	}
	entry.width = l.width
	entry.version = finalVersion
	entry.frozen = frozen
	entry.content = rendered
	entry.lines = lines
	entry.height = height
	return entry
}

// invalidateAll drops every cache entry. Called on width changes.
func (l *List) invalidateAll() {
	for k := range l.cache {
		delete(l.cache, k)
	}
	l.totalHeightValid = false
}

// Invalidate drops the cache entry for the given item, forcing a
// re-render on the next getItem call. No-op if the item is not in
// the cache.
func (l *List) Invalidate(item Item) {
	delete(l.cache, item)
}

// InvalidateFrozen drops the frozen flag (and stored content) for the
// given item. Equivalent to Invalidate but exposed under the F6
// frozen-items vocabulary so external callers can express intent.
func (l *List) InvalidateFrozen(item Item) {
	delete(l.cache, item)
}

// retainCacheFor drops every cache entry whose key is not in the given
// item set. Used by SetItems to keep entries for stable items while
// dropping entries for removed ones.
func (l *List) retainCacheFor(items []Item) {
	if len(l.cache) == 0 {
		return
	}
	keep := make(map[Item]struct{}, len(items))
	for _, it := range items {
		keep[it] = struct{}{}
	}
	for k := range l.cache {
		if _, ok := keep[k]; !ok {
			delete(l.cache, k)
		}
	}
}

// BeginSelectionDrag marks the items in the inclusive [startIdx, endIdx]
// range as un-freezable for the duration of an active selection drag.
// Frozen entries inside the range are dropped so the next render
// reflects live selection-overlay output. The corresponding
// EndSelectionDrag clears the suppression set and lets items
// re-freeze on their next render. Indices outside the items slice
// are clipped silently.
func (l *List) BeginSelectionDrag(startIdx, endIdx int) {
	if len(l.items) == 0 {
		return
	}
	if startIdx > endIdx {
		startIdx, endIdx = endIdx, startIdx
	}
	startIdx = max(startIdx, 0)
	endIdx = min(endIdx, len(l.items)-1)
	for i := startIdx; i <= endIdx; i++ {
		it := l.items[i]
		l.freezeSuppressed[it] = struct{}{}
		// Drop any cached frozen entry so the next render rebuilds
		// it as a live (un-frozen) entry that picks up the
		// selection overlay.
		if entry, ok := l.cache[it]; ok && entry.frozen {
			delete(l.cache, it)
		}
	}
}

// EndSelectionDrag clears the selection-drag freeze suppression. Items
// inside the previous range will re-freeze on their next render once
// their Finished() reports true again.
func (l *List) EndSelectionDrag() {
	for k := range l.freezeSuppressed {
		delete(l.freezeSuppressed, k)
		// Drop the cache entry so the next render produces a clean
		// (un-highlighted) frozen entry.
		delete(l.cache, k)
	}
}
