package list

import (
	"strings"
)

// List represents a list of items that can be lazily rendered. A list is
// always rendered like a chat conversation where items are stacked vertically
// from top to bottom.
type List struct {
	// Viewport size
	width, height int

	// Items in the list
	items []Item

	// Gap between items (0 or less means no gap)
	gap int

	// show list in reverse order
	reverse bool

	// Focus and selection state
	focused     bool
	selectedIdx int // The current selected index -1 means no selection

	// offsetIdx is the index of the first visible item in the viewport.
	offsetIdx int
	// offsetLine is the number of lines of the item at offsetIdx that are
	// scrolled out of view (above the viewport).
	// It must always be >= 0.
	offsetLine int

	// renderCallbacks is a list of callbacks to apply when rendering items.
	renderCallbacks []func(idx, selectedIdx int, item Item) Item

	// totalHeightCache is a cached value of the total rendered height of
	// all items. It is invalidated whenever the item set changes or the
	// viewport width changes (which can alter per-item line counts).
	totalHeightCache int
	totalHeightValid bool

	// cache is the F6 list-level render memo, keyed by item pointer.
	// Each entry stores the rendered content, a pre-split slice of
	// lines (so AtBottom / Render / VisibleItemIndices /
	// findItemAtY all share one render per frame), the height, and
	// the keys that govern invalidation (width and version). The
	// frozen flag mirrors §4.5.1: once a Finished() item is
	// rendered, subsequent draws return the stored output verbatim
	// without calling back into Render.
	cache map[Item]*listCacheEntry

	// freezeSuppressed marks items the list must not freeze on the
	// next render even when their Finished() reports true. This is
	// the §4.5.1 selection-drag escape hatch (option (a)): items
	// inside an active selection range render as live items so that
	// per-line highlight overlays land on the latest content. Cleared
	// on EndSelectionDrag.
	freezeSuppressed map[Item]struct{}
}

// listCacheEntry is the per-item entry in the list-level render memo.
type listCacheEntry struct {
	width   int
	version uint64
	frozen  bool
	content string
	lines   []string
	height  int
}

// renderedItem is the legacy view of a cached entry returned by getItem.
// Internal callers that don't need the line slice keep using this
// shape; functions that walk lines (Render) take the slice off the
// cache entry directly.
type renderedItem struct {
	content string
	height  int
}

// NewList creates a new lazy-loaded list.
func NewList(items ...Item) *List {
	l := new(List)
	l.items = items
	l.selectedIdx = -1
	l.cache = make(map[Item]*listCacheEntry)
	l.freezeSuppressed = make(map[Item]struct{})
	return l
}

// RenderCallback defines a function that can modify an item before it is
// rendered.
type RenderCallback func(idx, selectedIdx int, item Item) Item

// RegisterRenderCallback registers a callback to be called when rendering
// items. This can be used to modify items before they are rendered.
func (l *List) RegisterRenderCallback(cb RenderCallback) {
	l.renderCallbacks = append(l.renderCallbacks, cb)
}

// SetSize sets the size of the list viewport. A width change drops the
// entire render cache because every entry's wrapped output depends on
// width; a height-only change is a no-op for the cache.
func (l *List) SetSize(width, height int) {
	if l.width != width {
		l.invalidateAll()
	}
	l.width = width
	l.height = height
}

// SetGap sets the gap between items.
func (l *List) SetGap(gap int) {
	l.gap = gap
}

// Gap returns the gap between items.
func (l *List) Gap() int {
	return l.gap
}

// AtBottom returns whether the list is showing the last item at the bottom.
func (l *List) AtBottom() bool {
	if len(l.items) == 0 {
		return true
	}

	// Calculate the height from offsetIdx to the end. The comparison is
	// against the visible height (totalHeight minus the lines of the first
	// item that are scrolled out of view), otherwise a first item taller
	// than the viewport reports "not at bottom" while it is in fact
	// pinned there.
	var totalHeight int
	for idx := l.offsetIdx; idx < len(l.items); idx++ {
		if totalHeight-l.offsetLine > l.height {
			// No need to calculate further, we're already past the viewport height
			return false
		}
		item := l.getItem(idx)
		itemHeight := item.height
		if l.gap > 0 && idx > l.offsetIdx {
			itemHeight += l.gap
		}
		totalHeight += itemHeight
	}

	return totalHeight-l.offsetLine <= l.height
}

// SetReverse shows the list in reverse order.
func (l *List) SetReverse(reverse bool) {
	l.reverse = reverse
}

// Width returns the width of the list viewport.
func (l *List) Width() int {
	return l.width
}

// Height returns the height of the list viewport.
func (l *List) Height() int {
	return l.height
}

// Len returns the number of items in the list.
func (l *List) Len() int {
	return len(l.items)
}

// TotalHeight returns the total height of all items in the list.
// The result is cached and only recomputed when the item set or
// viewport width changes.
func (l *List) TotalHeight() int {
	if l.totalHeightValid {
		return l.totalHeightCache
	}
	total := 0
	for idx := range l.items {
		entry := l.renderItemEntry(idx)
		if entry == nil {
			continue
		}
		total += entry.height
		if l.gap > 0 && idx < len(l.items)-1 {
			total += l.gap
		}
	}
	l.totalHeightCache = total
	l.totalHeightValid = true
	return total
}

// Prewarm renders items in the range [from, from+batch) into the width
// cache and returns the next index to warm (len(items) when done). It lets
// a caller populate the per-item render cache incrementally across frames
// so a later TotalHeight is instant instead of rendering everything at
// once. Rendering is otherwise identical to what TotalHeight would do.
func (l *List) Prewarm(from, batch int) int {
	if from < 0 {
		from = 0
	}
	end := min(from+batch, len(l.items))
	for idx := from; idx < end; idx++ {
		l.renderItemEntry(idx)
	}
	return end
}

// Overflows reports whether the items' total height exceeds the given
// viewport height. It walks from the bottom and stops as soon as the
// threshold is crossed, so for content taller than the viewport (the
// common case) it renders only a viewport's worth of items rather than
// all of them — much cheaper than TotalHeight when only the boolean is
// needed (e.g. deciding whether a scrollbar is required).
func (l *List) Overflows(height int) bool {
	total := 0
	for idx := len(l.items) - 1; idx >= 0; idx-- {
		total += l.getItem(idx).height
		if l.gap > 0 && idx < len(l.items)-1 {
			total += l.gap
		}
		if total > height {
			return true
		}
	}
	return false
}

// ItemsVersion folds every item's version into one number. It changes
// whenever any item in this list mutates in a way that affects its rendered
// output, since that is exactly the contract [Versioned.Bump] carries.
//
// It reads one field per item and renders nothing, so it is cheap enough to
// call once per frame. Callers that memoize a whole rendered frame fold it
// into their cache key to pick up item mutations they do not otherwise know
// about.
func (l *List) ItemsVersion() uint64 {
	var v uint64
	for _, item := range l.items {
		v = v*31 + item.Version()
	}
	return v
}

// ScrollPosition returns the index of the first visible item and the line
// offset into it. Unlike Offset it is O(1) and does not render items.
func (l *List) ScrollPosition() (offsetIdx, offsetLine int) {
	return l.offsetIdx, l.offsetLine
}

// Offset returns the current scroll offset in lines from the top.
func (l *List) Offset() int {
	offset := 0
	for idx := 0; idx < l.offsetIdx; idx++ {
		item := l.getItem(idx)
		offset += item.height
		if l.gap > 0 && idx < len(l.items)-1 {
			offset += l.gap
		}
	}
	offset += l.offsetLine
	return offset
}

// Render renders the list and returns the visible lines.
//
// F7: per-item slicing is bounded by the remaining viewport budget so
// per-frame work is O(viewport) rather than O(total item heights).
// We never append beyond l.height lines to the output buffer; the
// final trim is therefore unnecessary. Reverse mode applies the same
// final reversal as before, which is byte-identical because the
// pre-F7 trim happened at the tail of the joined buffer (the same
// lines we now drop implicitly per item).
func (l *List) Render() string {
	if len(l.items) == 0 {
		return ""
	}

	budget := max(l.height, 0)
	lines := make([]string, 0, budget)
	currentIdx := l.offsetIdx
	currentOffset := l.offsetLine

	for currentIdx < len(l.items) {
		remaining := budget - len(lines)
		if remaining <= 0 {
			break
		}

		entry := l.renderItemEntry(currentIdx)
		if entry == nil {
			break
		}
		itemLines := entry.lines
		itemHeight := len(itemLines)

		if currentOffset >= 0 && currentOffset < itemHeight {
			// Append only the visible slice that fits in the
			// remaining viewport budget. Anything past the
			// budget would be discarded by the pre-F7 tail
			// trim, so skipping the append here is
			// byte-identical and bounded.
			visible := itemLines[currentOffset:]
			if len(visible) > remaining {
				visible = visible[:remaining]
			}
			lines = append(lines, visible...)

			// Gap rows after the item, capped to the
			// remaining budget so a 30k-line item with a
			// trailing gap can't push past the viewport.
			if l.gap > 0 {
				gapBudget := min(budget-len(lines), l.gap)
				for range gapBudget {
					lines = append(lines, "")
				}
			}
		} else {
			// offsetLine starts inside the gap.
			gapOffset := currentOffset - itemHeight
			gapRemaining := l.gap - gapOffset
			if gapRemaining > 0 {
				gapBudget := min(budget-len(lines), gapRemaining)
				for range gapBudget {
					lines = append(lines, "")
				}
			}
		}

		currentIdx++
		currentOffset = 0 // Reset offset for subsequent items.
	}

	l.height = budget

	if l.reverse {
		// Reverse the lines so the list renders bottom-to-top.
		for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
			lines[i], lines[j] = lines[j], lines[i]
		}
	}

	return strings.Join(lines, "\n")
}

// PrependItems prepends items to the list.
func (l *List) PrependItems(items ...Item) {
	l.items = append(items, l.items...)

	// Keep view position relative to the content that was visible
	l.offsetIdx += len(items)

	// Update selection index if valid
	if l.selectedIdx != -1 {
		l.selectedIdx += len(items)
	}
	l.totalHeightValid = false
}

// SetItems sets the items in the list. Cache entries for items that
// remain after the swap are preserved; entries for removed items are
// dropped.
func (l *List) SetItems(items ...Item) {
	l.items = items
	l.selectedIdx = min(l.selectedIdx, len(l.items)-1)
	l.offsetIdx = max(min(l.offsetIdx, len(l.items)-1), 0)
	l.offsetLine = 0
	l.retainCacheFor(items)
	l.totalHeightValid = false
}

// ReplaceItems swaps in a new item slice at the given scroll position and
// selection, where SetItems would reset the scroll to the top of the
// offset item.
func (l *List) ReplaceItems(items []Item, offsetIdx, offsetLine, selectedIdx int) {
	l.SetItems(items...)
	if len(items) == 0 {
		return
	}
	l.offsetIdx = min(max(offsetIdx, 0), len(items)-1)
	l.offsetLine = max(offsetLine, 0)
	l.selectedIdx = min(selectedIdx, len(items)-1)
}

// AppendItems appends items to the list.
func (l *List) AppendItems(items ...Item) {
	l.items = append(l.items, items...)
	l.totalHeightValid = false
}

// RemoveItem removes the item at the given index from the list.
func (l *List) RemoveItem(idx int) {
	if idx < 0 || idx >= len(l.items) {
		return
	}

	removed := l.items[idx]

	// Remove the item
	l.items = append(l.items[:idx], l.items[idx+1:]...)

	// Drop the cache entry for the removed item; entries for stable
	// items stay valid because they are keyed by pointer, not index.
	delete(l.cache, removed)
	delete(l.freezeSuppressed, removed)

	// Adjust selection if needed
	if l.selectedIdx == idx {
		l.selectedIdx = -1
	} else if l.selectedIdx > idx {
		l.selectedIdx--
	}

	// Adjust offset if needed
	if l.offsetIdx > idx {
		l.offsetIdx--
	} else if l.offsetIdx == idx && l.offsetIdx >= len(l.items) {
		l.offsetIdx = max(0, len(l.items)-1)
		l.offsetLine = 0
	}
	l.totalHeightValid = false
}

// Focused returns whether the list is focused.
func (l *List) Focused() bool {
	return l.focused
}

// Focus sets the focus state of the list.
func (l *List) Focus() {
	l.focused = true
}

// Blur removes the focus state from the list.
func (l *List) Blur() {
	l.focused = false
}

// ItemAt returns the item at the given index.
func (l *List) ItemAt(index int) Item {
	if index < 0 || index >= len(l.items) {
		return nil
	}
	return l.items[index]
}
