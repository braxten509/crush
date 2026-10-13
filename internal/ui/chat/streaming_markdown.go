package chat

import (
	"strings"

	"charm.land/glamour/v2"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/x/ansi"
)

// streamingMarkdown caches a "stable prefix" glamour render so each
// streaming flush only re-renders the trailing portion of the
// document. F8 of docs/notes/2026-05-12-chat-rendering-perf.md.
//
// The boundary between "stable" and "trailing" is detected by
// [findSafeMarkdownBoundary]: a position immediately after a blank
// line at which we can prove no markdown construct is open
// (fenced code block, list, table, block quote, setext header).
//
// Two renders concatenated are NOT generally equal to a single
// render of the whole document — glamour's wrap state is reset
// between calls. The boundary check is therefore deliberately
// conservative; whenever it has the slightest doubt the call
// falls back to a full render and the cache is left untouched.
//
// Invariants:
//
//   - stablePrefix is always a literal byte prefix of the most
//     recently rendered content. If a new content does not have
//     stablePrefix as its prefix the cache is dropped.
//   - stablePrefixRender is the glamour render of stablePrefix
//     alone, with surrounding whitespace trimmed for clean
//     concatenation.
//   - width is the glamour wrap width that produced
//     stablePrefixRender. A width change drops the cache.
type streamingMarkdown struct {
	width              int
	stablePrefix       string
	stablePrefixRender string
	// Cached cumulative state at the stable prefix boundary.
	// Used by findBoundaryAfter to validate new boundary candidates
	// without re-scanning the entire prefix from the start.
	// baseFenceCount is always even (safe boundaries require even
	// fence parity), so the delta scan always starts outside a fence.
	baseFenceCount    int
	baseHasListMarker bool

	// stablePrefixLines is the line count of stablePrefixRender, and
	// lastLines the line count of whatever the most recent Render
	// returned. Callers need the count to size a truncation hint;
	// tracking it alongside the prefix keeps that O(trail) rather than
	// a fresh O(document) scan on every flush.
	stablePrefixLines int
	lastLines         int
}

// LastLines reports the number of lines in the string the most recent
// [streamingMarkdown.Render] returned, with surrounding whitespace
// trimmed. Equivalent to countLines on that trimmed string, without
// rescanning it.
func (s *streamingMarkdown) LastLines() int {
	return s.lastLines
}

// Reset drops every cached field. After Reset the next Render call
// is guaranteed to be a full render.
func (s *streamingMarkdown) Reset() {
	s.width = 0
	s.stablePrefix = ""
	s.stablePrefixRender = ""
	s.baseFenceCount = 0
	s.baseHasListMarker = false
	s.stablePrefixLines = 0
	s.lastLines = 0
}

// Render returns the glamour render of content at the given width,
// reusing the cached stable-prefix render when it is safe to do so.
// On any uncertainty the call falls back to a full render via
// renderer and leaves the cache untouched (or drops it).
//
// The returned string has its trailing newline trimmed to match
// the existing renderMarkdown contract on AssistantMessageItem.
//
// Concurrency: glamour's Render is stateful and not safe for
// concurrent invocation on a shared renderer. Crush's TUI is
// single-threaded so production never contends, but parallel
// callers (most notably the test suite) must serialize. We hold
// [common.LockMarkdownRenderer] for the entire prefix +
// trailing render sequence so other goroutines cannot interleave
// their own Render calls and corrupt goldmark's BlockStack.
func (s *streamingMarkdown) Render(content string, width int, renderer *glamour.TermRenderer) string {
	mu := common.LockMarkdownRenderer(renderer)
	mu.Lock()
	defer mu.Unlock()
	full := func() string {
		out, err := renderer.Render(content)
		if err != nil {
			out = content
		} else {
			out = strings.TrimSuffix(out, "\n")
		}
		// Counted against the trimmed form: that is what the
		// windowing callers measure, and the glue paths below
		// already return trimmed output.
		s.lastLines = countLines(strings.TrimSpace(out))
		return out
	}

	// Width change OR content not a prefix-extension: drop cache,
	// full render, optionally try to seed a fresh boundary on this
	// call (step "f" in the design note).
	if width != s.width || !strings.HasPrefix(content, s.stablePrefix) {
		s.Reset()
		s.width = width
		out := full()
		s.tryAdvanceFromEmpty(content, width, renderer)
		return out
	}

	// Incremental boundary search: only scan the delta after the
	// stable prefix. The cached cumulative state (baseFenceCount,
	// baseHasListMarker) lets us validate candidates in O(delta)
	// instead of re-scanning the entire prefix. See CHARM-1785.
	boundary := s.findBoundaryAfter(content)
	if boundary < 0 {
		// No safe boundary anywhere yet. Full render; do not
		// modify the cache (a future flush may find one).
		return full()
	}

	if boundary <= len(s.stablePrefix) {
		// Cached prefix already covers an at-least-as-late
		// boundary. Render the trailing partial fresh and glue.
		trail := content[len(s.stablePrefix):]
		return s.glue(s.stablePrefixRender, s.stablePrefixLines, s.renderTrailing(trail, renderer))
	}

	// boundary > len(stablePrefix): we have a NEW chunk of safe
	// content. Render the new chunk, append to stablePrefixRender,
	// promote the boundary, then render the remaining trail.
	newChunk := content[len(s.stablePrefix):boundary]
	newChunkRender := s.renderTrailing(newChunk, renderer)
	s.stablePrefixLines = glueLines(s.stablePrefixRender, s.stablePrefixLines, newChunkRender)
	s.stablePrefixRender = glueRenders(s.stablePrefixRender, newChunkRender)
	s.stablePrefix = content[:boundary]
	// Update cumulative state for the new stable prefix.
	s.baseFenceCount += countFenceLines(newChunk)
	s.baseHasListMarker = s.baseHasListMarker || chunkHasListMarker(newChunk)

	trail := content[boundary:]
	if trail == "" {
		// boundary == len(content): no trailing content. Returning
		// the cached prefix render directly is correct.
		s.lastLines = s.stablePrefixLines
		return s.stablePrefixRender
	}
	return s.glue(s.stablePrefixRender, s.stablePrefixLines, s.renderTrailing(trail, renderer))
}

// glue joins a prefix render to a trailing render and records the line
// count of the result, given the prefix's already-known count.
func (s *streamingMarkdown) glue(prefix string, prefixLines int, trail string) string {
	s.lastLines = glueLines(prefix, prefixLines, trail)
	return glueRenders(prefix, trail)
}

// glueLines returns the line count [glueRenders] would produce, without
// scanning the prefix.
func glueLines(prefix string, prefixLines int, trail string) int {
	if prefix == "" {
		return countLines(trail)
	}
	if trail == "" {
		return prefixLines
	}
	// glueRenders joins with a blank line between the two renders.
	return prefixLines + 1 + countLines(trail)
}

// tryAdvanceFromEmpty seeds the cache from a fresh state. We've
// already paid the cost of a full render of `content`; if there is
// a safe boundary inside it, render the prefix once more (cheap
// relative to the full render we just did) and cache it so the
// next flush can avoid the full work.
//
// This is the optional optimisation step "f" from the design
// note. We render the prefix separately rather than try to
// recover it from the full render output because two renders
// concatenated ≠ a single render of the whole, and we prefer the
// cached prefix render to be byte-for-byte what we'd produce on a
// future cached call.
func (s *streamingMarkdown) tryAdvanceFromEmpty(content string, width int, renderer *glamour.TermRenderer) {
	boundary := findSafeMarkdownBoundary(content)
	if boundary <= 0 {
		boundary = s.relaxedBoundary(content, 0)
	}
	if boundary <= 0 {
		return
	}
	prefix := content[:boundary]
	out, err := renderer.Render(prefix)
	if err != nil {
		return
	}
	s.stablePrefix = prefix
	s.stablePrefixRender = trimGlamourMargins(out)
	s.stablePrefixLines = countLines(s.stablePrefixRender)
	s.width = width
	// Seed cumulative state for incremental boundary search.
	s.baseFenceCount = countFenceLines(prefix)
	s.baseHasListMarker = chunkHasListMarker(prefix)
}

// findBoundaryAfter searches for the latest safe boundary in content
// that is strictly after the stable prefix. It uses the cached
// cumulative state (baseFenceCount, baseHasListMarker) to validate
// candidates without re-scanning the entire prefix, making the search
// O(delta) instead of O(n) per tick. See CHARM-1785.
//
// Returns -1 when no safe boundary exists after the stable prefix.
func (s *streamingMarkdown) findBoundaryAfter(content string) int {
	// When there is no stable prefix, fall back to the full scan.
	if len(s.stablePrefix) == 0 {
		if b := findSafeMarkdownBoundary(content); b > 0 {
			return b
		}
		return s.relaxedBoundary(content, 0)
	}

	// Scan blank-line candidates from latest to earliest, but only
	// those strictly after the stable prefix. Candidates at or
	// before the stable prefix are already covered by the cache.
	for p := blankLineBefore(content, len(content)); p > len(s.stablePrefix); p = blankLineBefore(content, p-1) {
		if s.isSafeBoundaryIncremental(content, p) {
			return p
		}
	}
	if b := s.relaxedBoundary(content, len(s.stablePrefix)); b > len(s.stablePrefix) {
		return b
	}
	// No new boundary found after the stable prefix. Return the
	// stable prefix length so the caller takes the "boundary <=
	// len(stablePrefix)" path (render trailing fresh, keep cache).
	return len(s.stablePrefix)
}

// relaxBoundaryAfter bounds how much unstable tail a flush will
// re-render. Prose holding no blank line never satisfies the
// blank-line predicate, so the cache never advances and every flush
// re-renders the whole document: O(n) per tick, O(n^2) over a turn. A
// reasoning trace that reaches a few hundred KB burns tens of GB of
// churn that way, which is enough to outrun the GC (#3162).
// Sized just above a prose paragraph so structured text never reaches
// it: the blank-line boundary fires first and the tail stays small.
// Only prose with no paragraph breaks at all gets cut here, and it has
// no reflow left to protect.
const relaxBoundaryAfter = 2 << 10

// relaxedBoundaryCandidates caps how many newline candidates one
// flush will validate, keeping the search cost flat when none of them
// pass.
const relaxedBoundaryCandidates = 8

// relaxedBoundary looks for a cut at a plain newline once the tail
// after `after` has outgrown [relaxBoundaryAfter]. Candidates are
// still validated by the same predicate the blank-line search uses,
// so every construct check survives; the only conservatism given up
// is paragraph wrapping across the cut, which costs one re-wrap at a
// point the reader has usually already scrolled past.
//
// Returns -1 while the tail is still small, and when no candidate
// validates — the caller then behaves exactly as it did before.
// Content with no newline at all has nothing to cut on and stays on
// the full-render path.
func (s *streamingMarkdown) relaxedBoundary(content string, after int) int {
	if len(content)-after <= relaxBoundaryAfter {
		return -1
	}
	tried := 0
	for p := newlineBefore(content, len(content)); p > after; p = newlineBefore(content, p-1) {
		if s.isSafeBoundaryIncremental(content, p) {
			return p
		}
		if tried++; tried >= relaxedBoundaryCandidates {
			break
		}
	}
	return -1
}

// newlineBefore returns the byte offset of the first character AFTER
// the latest newline that ends strictly before `until`, or -1 when
// there is none.
func newlineBefore(content string, until int) int {
	if until <= 0 {
		return -1
	}
	nl := strings.LastIndexByte(content[:until], '\n')
	if nl < 0 {
		return -1
	}
	return nl + 1
}

// isSafeBoundaryIncremental validates a boundary candidate at
// position p using the cached cumulative state plus a delta scan
// of content[len(stablePrefix):p]. This avoids the O(n) re-scan
// that isSafeBoundaryAt performs on every candidate.
func (s *streamingMarkdown) isSafeBoundaryIncremental(content string, p int) bool {
	delta := content[len(s.stablePrefix):p]

	// (2) Fence parity: base count + delta count must be even.
	if (s.baseFenceCount+countFenceLines(delta))%2 != 0 {
		return false
	}

	// (2b) HTML and link-ref hazards in the delta.
	if deltaHasHTMLorRef(delta) {
		return false
	}

	// (2b) List hazard: if a list marker exists anywhere in the
	// full prefix (base OR delta), the last non-blank line before
	// the boundary must not be an indented continuation paragraph.
	hasListMarker := s.baseHasListMarker || chunkHasListMarker(delta)
	if hasListMarker {
		lastLine := lastNonBlankLine(content[:p])
		if lastLine != "" && !isListItemMarker(strings.TrimLeft(lastLine, " \t")) {
			if len(lastLine) > 0 && (lastLine[0] == ' ' || lastLine[0] == '\t') {
				return false
			}
		}
	}

	// (3) Last non-blank line must not open a construct.
	lastLine := lastNonBlankLine(content[:p])
	if lastLine != "" && lineOpensConstruct(lastLine) {
		return false
	}

	// (4) Setext underline check.
	if rest := content[p:]; rest != "" {
		first := firstNonBlankLine(rest)
		if isSetextUnderlineCandidate(first) {
			return false
		}
	}

	return true
}

// deltaHasHTMLorRef reports whether the delta (text between the
// stable prefix and a boundary candidate) contains an HTML block
// opener or a link reference definition.
func deltaHasHTMLorRef(delta string) bool {
	inFence := false
	for line := range splitLines(delta) {
		if isFenceLine(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if isHTMLBlockOpener(line) || isLinkRefDefinition(line) {
			return true
		}
	}
	return false
}

// chunkHasListMarker reports whether any line in chunk is a
// list-item marker (outside fenced code blocks).
func chunkHasListMarker(chunk string) bool {
	inFence := false
	for line := range splitLines(chunk) {
		if isFenceLine(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if isListItemMarker(strings.TrimLeft(line, " \t")) {
			return true
		}
	}
	return false
}

// renderTrailing renders a trailing partial as a fresh glamour
// document and trims the surrounding whitespace so it can be
// concatenated to a cached prefix render without doubled blank
// lines.
func (s *streamingMarkdown) renderTrailing(text string, renderer *glamour.TermRenderer) string {
	if text == "" {
		return ""
	}
	out, err := renderer.Render(text)
	if err != nil {
		return text
	}
	return trimGlamourMargins(out)
}

// glueRenders concatenates two glamour-rendered fragments with a
// single blank line separator. Glamour outputs typically carry
// their own surrounding margins; trimming on both sides and
// gluing with "\n\n" prevents the visible double-margin seam.
//
// Empty fragments are tolerated so the same helper works for the
// "boundary == len(content)" path where there is no trailing
// segment.
func glueRenders(prefix, trail string) string {
	prefix = trimGlamourMargins(prefix)
	trail = trimGlamourMargins(trail)
	switch {
	case prefix == "" && trail == "":
		return ""
	case prefix == "":
		return trail
	case trail == "":
		return prefix
	default:
		return prefix + "\n\n" + trail
	}
}

// trimGlamourMargins strips leading and trailing whitespace
// (including newlines) from a glamour-rendered fragment.
// Glamour adds a leading blank line for documents that open with
// a heading or paragraph, plus a trailing newline; both must be
// removed before concatenation. Its blank margin lines are often
// styled (color codes around padding), so a line counts as blank by
// what it shows, not by its bytes; otherwise a styled margin
// survives and the glue adds a second blank line. A content line's own indentation (a table's leading space) is kept.
func trimGlamourMargins(s string) string {
	lines := strings.Split(s, "\n")
	blank := func(line string) bool { return strings.TrimSpace(ansi.Strip(line)) == "" }
	start, end := 0, len(lines)
	for start < end && blank(lines[start]) {
		start++
	}
	for end > start && blank(lines[end-1]) {
		end--
	}
	return strings.TrimRight(strings.Join(lines[start:end], "\n"), " \t")
}
