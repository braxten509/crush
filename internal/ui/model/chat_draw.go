package model

import (
	"image"
	"strings"
	"time"

	uv "github.com/charmbracelet/ultraviolet"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/x/ansi"
)

// resizeSettleDuration is how long after the last resize event the chat
// waits before it starts warming the message cache it skipped mid-drag.
const resizeSettleDuration = 120 * time.Millisecond

// warmBatchSize is how many messages the chat renders into the width cache
// per warming step. Kept small so no single step blocks the UI thread for
// more than a frame or so, even on slow-to-render items.
const warmBatchSize = 25

// warmStepBudget caps how long one warming step may render.
const warmStepBudget = 8 * time.Millisecond

// chatWarmMsg drives one incremental cache-warming step. The first one is
// delayed until the resize settles; the rest fire immediately, one per
// batch, so warming spreads across frames instead of blocking.
type chatWarmMsg struct {
	seq int // guards against stale timers from superseded resizes
}

// chatWarmCmd schedules the next warming step after delay (zero fires as
// soon as the runtime delivers it).
func chatWarmCmd(seq int, delay time.Duration) tea.Cmd {
	if delay <= 0 {
		return func() tea.Msg { return chatWarmMsg{seq: seq} }
	}
	return tea.Tick(delay, func(_ time.Time) tea.Msg {
		return chatWarmMsg{seq: seq}
	})
}

// chatDrawCache holds the pre-decoded form of the last list.Render output.
// The cache is keyed by the rendered string and the screen's width method
// (graphemes vs wcwidth pick different decoders inside ultraviolet's
// printString, so a cached buffer is only valid for the method it was
// decoded with). The cached buffer is independent of the draw area, so
// resize / scroll changes that produce the same string still hit. We cannot
// use uv.StyledString.Lines because it bottoms out at the first iteration
// against a zero-bounds rectangle (see ultraviolet styled.go line 45 — the
// shared printString loop's `y >= bounds.Max.Y` exit applies to the
// line-building branch too). A Buffer of the rendered text's natural
// dimensions is the cheapest correct shape: StyledString.Draw runs once on
// miss to populate it, and Buffer.Draw is an O(cells) cell copy with no
// ANSI re-parse on hit.
type chatDrawCache struct {
	rendered string
	method   ansi.Method
	buf      uv.ScreenBuffer
}

// Draw renders the chat UI component to the screen and the given area.
//
// The list's rendered output is cached in decoded form (see chatDrawCache) so
// that frames with byte-identical content skip the ANSI reparse that
// uv.StyledString.Draw performs on every call. The cache is keyed by the
// rendered string and the screen's width method; area / scroll changes do not
// invalidate it.
func (m *Chat) Draw(scr uv.Screen, area uv.Rectangle) {
	// Determine scrollbar visibility. Skip it entirely while resizing: the
	// thumb needs the exact total height (O(N) after a width change), which
	// is the dominant resize cost. It returns once the resize settles and
	// the cache has been warmed. The needs-scrollbar test itself uses the
	// cheap bounded overflow check.
	listHeight := m.list.Height() - 1
	needsScrollbar := false
	if !m.resizing {
		needsScrollbar = m.list.Overflows(m.list.Height())
	}

	// Determine visibility based on scrollbar mode.
	showScrollbar := false
	switch m.scrollbarMode {
	case config.ScrollbarAlways:
		showScrollbar = needsScrollbar
	case config.ScrollbarDefault:
		showScrollbar = needsScrollbar && m.scrollbarVisible
	case config.ScrollbarNever:
		showScrollbar = false
	}

	// Reserve space for scrollbar only when visible.
	scrollbarWidth := 0
	if showScrollbar {
		scrollbarWidth = 1
	}

	// Adjust list width to reserve space for scrollbar.
	listArea := area
	if scrollbarWidth > 0 {
		listArea.Max.X -= scrollbarWidth
	}

	rendered := m.list.Render()
	// If we're in follow mode but the render revealed we're no longer at
	// the bottom (e.g. streaming content grew an item), re-anchor and
	// re-render so the view stays pinned to the end.
	if m.follow && !m.list.AtBottom() {
		m.list.ScrollToBottom()
		rendered = m.list.Render()
	}
	method, ok := scr.WidthMethod().(ansi.Method)
	if !ok {
		// Width method isn't an ansi.Method (unlikely in practice — both
		// TerminalScreen and ScreenBuffer store ansi.Method). Fall back
		// to the uncached path so behavior matches upstream exactly.
		uv.NewStyledString(rendered).Draw(scr, listArea)
	} else {
		if m.drawCache == nil ||
			m.drawCache.rendered != rendered ||
			m.drawCache.method != method {
			m.drawCache = newChatDrawCache(rendered, method)
		}
		drawCachedBuffer(scr, listArea, m.drawCache.buf)
	}

	// Draw scrollbar if visible and needed. Only reached when not resizing
	// (showScrollbar requires it), so TotalHeight is already computed and
	// cached above.
	m.scrollbarEdge = area.Dx() - 1
	if scrollbarWidth > 0 {
		scrollbar := common.Scrollbar(m.com.Styles, listHeight, m.list.TotalHeight()-1, listHeight, m.list.Offset())
		if scrollbar != "" {
			scrollbarArea := image.Rectangle{
				Min: image.Point{X: area.Max.X - scrollbarWidth, Y: area.Min.Y},
				Max: image.Point{X: area.Max.X, Y: area.Max.Y},
			}
			uv.NewStyledString(scrollbar).Draw(scr, scrollbarArea)
		}
	}
}

// newChatDrawCache builds a chatDrawCache for the given rendered string by
// running uv.StyledString.Draw into a fresh buffer sized to the text's
// natural bounds under the active width method. This is the only place
// ANSI decoding happens for cached frames — subsequent draws reuse buf
// via drawCachedBuffer.
//
// We can't use uv.StyledString.Bounds() here: it is hard-coded to
// ansi.GraphemeWidth, while StyledString.Draw lays cells using the
// destination buffer's WidthMethod (which we capture in `method`). For
// strings where graphemes and wcwidth disagree (emoji ZWJ sequences,
// some CJK, certain combining marks) the two answers diverge, leaving
// the cached buffer either too small (trailing cells dropped on hit) or
// too large (dead cells past the live content). Computing dimensions
// with `method.StringWidth` per line matches what printString tallies
// cell-by-cell, since both decode ANSI sequences and use the same width
// method.
func newChatDrawCache(rendered string, method ansi.Method) *chatDrawCache {
	w, h := renderedBounds(rendered, method)
	if w <= 0 {
		w = 1
	}
	if h <= 0 {
		h = 1
	}
	buf := uv.NewScreenBuffer(w, h)
	buf.Method = method
	uv.NewStyledString(rendered).Draw(buf, buf.Bounds())
	return &chatDrawCache{
		rendered: rendered,
		method:   method,
		buf:      buf,
	}
}

// renderedBounds returns the (width, height) cell extent of rendered
// when laid out by method. Width is the widest line's StringWidth (which
// strips ANSI sequences and tallies cells via method, exactly like
// printString); height is the line count. Both match what
// uv.StyledString.Draw will write into a buffer whose WidthMethod is
// method, so the cache buffer is always sized to fit the live content.
func renderedBounds(rendered string, method ansi.Method) (w, h int) {
	for line := range strings.SplitSeq(rendered, "\n") {
		w = max(w, method.StringWidth(line))
		h++
	}
	return w, h
}

// drawCachedBuffer blits a previously-decoded buffer into scr at area,
// mirroring uv.StyledString.Draw's screen-mode behavior for the default
// Wrap=false, Tail="" case. The clear loop matches StyledString.Draw line
// 51-56; the buf.Draw call replaces the per-cell ANSI decode that
// printString does on every uncached frame with a pure cell copy.
func drawCachedBuffer(scr uv.Screen, area uv.Rectangle, buf uv.ScreenBuffer) {
	// Clear the area first to match StyledString.Draw — leftover cells
	// from a previous frame outside the new content must be zeroed,
	// because Buffer.Draw skips empty cells (it doesn't clear).
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			scr.SetCell(x, y, nil)
		}
	}
	buf.Draw(scr, area)
}

// BeginResize marks the chat as actively resizing so the next draws skip
// the full-height scan (and the scrollbar), reflowing only the visible
// items. It returns a command that, once resizing settles, starts warming
// the cache so the scrollbar can recompute without blocking.
func (m *Chat) BeginResize() tea.Cmd {
	m.resizing = true
	m.resizeSettleSeq++
	m.warmNext = 0
	return chatWarmCmd(m.resizeSettleSeq, resizeSettleDuration)
}

// BeginWarm starts warming the cache right away, like a settled resize:
// draws skip the full-height scan and the scrollbar until every message has
// been rendered once. Used after a session load, whose first scroll would
// otherwise render the whole history at once.
func (m *Chat) BeginWarm() tea.Cmd {
	m.resizing = true
	m.resizeSettleSeq++
	m.warmNext = 0
	return chatWarmCmd(m.resizeSettleSeq, 0)
}

// WarmStep renders the next batch of messages into the width cache and
// returns a command to continue warming plus whether warming finished. On
// completion the resize suppression is cleared so the next draw recomputes
// the (now instant) total height and scrollbar. A stale seq — from a resize
// that has since been superseded — is a no-op returning (nil, false).
func (m *Chat) WarmStep(seq int) (cmd tea.Cmd, done bool) {
	if seq != m.resizeSettleSeq {
		return nil, false
	}
	// A batch stops early once it used its time budget: a few large
	// messages (long diffs, big markdown) must not stall one frame.
	deadline := time.Now().Add(warmStepBudget)
	end := min(m.warmNext+warmBatchSize, m.list.Len())
	for m.warmNext < end {
		m.warmNext = m.list.Prewarm(m.warmNext, 1)
		if time.Now().After(deadline) {
			break
		}
	}
	if m.warmNext >= m.list.Len() {
		m.resizing = false
		return nil, true
	}
	return chatWarmCmd(seq, 0), false
}
