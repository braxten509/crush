package chat

import (
	"hash/fnv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/anim"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/list"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
)

// assistantMessageTruncateFormat is the text shown when an assistant message is
// truncated in the collapsed state.
const assistantMessageTruncateFormat = "… (%d lines hidden) [click or space to expand]"

// assistantMessageTailWindowFormat is shown above a tail-windowed thinking
// block to advertise that earlier lines exist and that the user can
// promote the view to a full expansion. The promotion is wired through
// the existing ToggleExpanded path (click / space) — F5 deliberately
// does not add a new keybinding.
const assistantMessageTailWindowFormat = "… %d earlier lines hidden [click or space for full view]"

// maxCollapsedThinkingHeight defines the maximum height of the thinking
const maxCollapsedThinkingHeight = 10

// Default copy for a provider-refusal banner. The agent persists only
// the FinishReasonContentFilter reason; the TUI owns this text and
// fills it in when the finish part carries no message/details (the
// normal live path, and restored sessions). Kept here as the single
// source of truth so the render path and tests cannot drift apart.
const (
	refusalTagLabel = "REFUSED"
	refusalTitle    = "Model refused to continue"
	refusalDetails  = "The provider's safety classifier stopped this response before any usable content was produced. Rephrase the request, start a fresh session, or try a different model."
)

// maxExpandedThinkingTailLines is the F5 tail-window cap. When the user
// expands a thinking block whose post-glamour line count exceeds this
// threshold, only the last N lines are shown with an affordance line
// indicating how many earlier lines are hidden. Clicking / pressing
// space again promotes the view to a full expansion. The slice is
// taken AFTER glamour render (not before) so fenced code blocks,
// lists, and tables are not torn at arbitrary boundaries.
const maxExpandedThinkingTailLines = 200

// thinkingViewMode is the F5 three-state view machine for the thinking
// block. ToggleExpanded cycles
// collapsed → tail-window → full-expanded → collapsed, skipping the
// tail-window step when the rendered thinking fits within the cap so
// short blocks still toggle in two clicks.
type thinkingViewMode uint8

const (
	thinkingCollapsed thinkingViewMode = iota
	thinkingTailWindow
	thinkingFullExpanded
)

// AssistantMessageItem represents an assistant message in the chat UI.
//
// This item includes thinking, and the content but does not include the tool calls.
type AssistantMessageItem struct {
	*list.Versioned
	*highlightableMessageItem
	*cachedMessageItem
	*focusableMessageItem

	message           *message.Message
	sty               *styles.Styles
	anim              *anim.Anim
	activity          activityTimer
	thinkingViewMode  thinkingViewMode
	hideReasoning     bool
	thinkingBoxHeight int // Tracks the rendered thinking box height for click detection.

	// bulletLine is the rendered line that opens the reply text and gets
	// the "•" marker in its gutter, or -1 when there is no reply text.
	bulletLine int

	// planAgent marks this item as plan-agent output. While the plan
	// streams (the message is not finished) and the plan-start marker
	// has arrived, the content renders as an open plan card: top and
	// side borders only, with the bottom border withheld until the
	// plan-ready marker lands.
	planAgent bool

	// superseded marks a step with something newer after it in the chat.
	// Only the end of the chat says what the agent is doing now, so a
	// superseded step shows no live status of its own.
	superseded bool
	// compactingEarlier says an earlier, superseded step is compacting,
	// so this live step reports that instead of "Thinking".
	compactingEarlier bool

	// Incremental FNV-64a hash of the thinking text. Avoids
	// re-hashing the entire accumulated text on every streaming
	// tick. thinkingHashSample holds a short prefix of the hashed
	// text so we can detect divergence (e.g. a user retry that
	// rewrites the thinking from scratch) without re-hashing the
	// whole thing. See CHARM-1785.
	thinkingHash       uint64
	thinkingHashLen    int
	thinkingHashSample string

	// Per-section render caches. Splitting these out means content
	// streaming does not invalidate the (often expensive) thinking
	// render, and vice versa.
	thinkingSec assistantSection
	contentSec  assistantSection
	errorSec    assistantSection

	// streamingContent caches a "stable prefix" glamour render of
	// the assistant content body so each streaming flush only
	// re-renders the trailing partial. F8 of
	// docs/notes/2026-05-12-chat-rendering-perf.md. See
	// streaming_markdown.go for the full algorithm.
	streamingContent streamingMarkdown

	// streamingThinking applies the same stable-prefix caching to
	// the thinking/reasoning section. Without this, every streaming
	// delta forces a full glamour re-render of the entire accumulated
	// thinking text, which burns CPU and starves the terminal emulator
	// during long reasoning traces.
	streamingThinking streamingMarkdown

	// streamingPlan applies the same stable-prefix caching to the plan
	// card while the plan streams, so each streaming flush only
	// re-renders the trailing partial of the plan document. Kept apart
	// from streamingContent because it renders through the plan
	// renderer, whose cached prefix is not interchangeable.
	streamingPlan streamingMarkdown
}

var _ Expandable = (*AssistantMessageItem)(nil)

// NewAssistantMessageItem creates a new AssistantMessageItem.
func NewAssistantMessageItem(sty *styles.Styles, message *message.Message) MessageItem {
	return newAssistantMessageItem(sty, message, true)
}

// Keep the low-level renderer separate from the chat's visibility policy.
// All normal chat items hide reasoning, regardless of provider or view mode.
func newAssistantMessageItem(sty *styles.Styles, message *message.Message, hideReasoning bool) MessageItem {
	v := list.NewVersioned()
	a := &AssistantMessageItem{
		Versioned:                v,
		highlightableMessageItem: defaultHighlighter(sty, v),
		cachedMessageItem:        &cachedMessageItem{},
		focusableMessageItem:     newFocusableMessageItem(v),
		message:                  message,
		hideReasoning:            hideReasoning,
		sty:                      sty,
	}

	a.anim = anim.New(anim.Settings{
		ID:          a.ID(),
		Size:        15,
		GradColorA:  sty.WorkingGradFromColor,
		GradColorB:  sty.WorkingGradToColor,
		LabelColor:  sty.WorkingLabelColor,
		CycleColors: true,
		Suffix: func() string {
			return a.activity.elapsed(time.Now())
		},
		SuffixColor: sty.WorkingTimerColor,
	})
	a.syncActivity(time.Now())
	return a
}

// Spinning implements [Animatable].
func (a *AssistantMessageItem) Spinning() bool {
	return a.isSpinning()
}

// restyleAnim implements [animRestyler].
func (a *AssistantMessageItem) restyleAnim() {
	a.anim.SetColors(
		a.sty.WorkingLabelColor,
		a.sty.WorkingGradFromColor,
		a.sty.WorkingGradToColor,
		a.sty.WorkingTimerColor,
	)
}

// Advance implements [Animatable].
func (a *AssistantMessageItem) Advance() bool {
	a.syncActivity(time.Now())
	if !a.isSpinning() || !a.anim.Advance() {
		return false
	}
	// Bump the F6 list-cache version so the next draw re-renders
	// this item: a spinner frame mutates anim's internal counter,
	// which changes the rendered output but is invisible to the
	// per-section content hashes. Without the bump the list cache
	// would serve the previously rendered frame indefinitely and
	// the spinner would appear frozen.
	a.Bump()
	return true
}

// ID implements MessageItem.
func (a *AssistantMessageItem) ID() string {
	return a.message.ID
}

// RawRender implements [MessageItem].
func (a *AssistantMessageItem) RawRender(width int) string {
	cappedWidth := cappedMessageWidth(width)
	a.bulletLine = -1

	var spinner string
	if a.isSpinning() {
		spinner = a.renderSpinning()
	}

	if a.message.IsCompacting || a.message.IsSummaryMessage {
		if spinner != "" {
			return spinner
		}
		if a.message.IsSummaryMessage {
			switch a.message.FinishReason() {
			case message.FinishReasonError:
				return a.renderError(cappedWidth)
			case message.FinishReasonCanceled:
				return a.sty.Messages.ThinkingFooterTitle.Render("Compaction canceled")
			default:
				return a.sty.Messages.ThinkingFooterTitle.Render("Conversation compacted")
			}
		}
	}

	content, height := a.renderMessageContent(cappedWidth)
	highlightedContent := a.renderHighlighted(content, cappedWidth, height)
	if spinner != "" {
		if highlightedContent != "" {
			highlightedContent += "\n\n"
		}
		return highlightedContent + spinner
	}

	return highlightedContent
}

// Render implements MessageItem.
func (a *AssistantMessageItem) Render(width int) string {
	// XXX: Here, we're manually applying the focused/blurred styles because
	// using lipgloss.Render can degrade performance for long messages due to
	// it's wrapping logic.
	// We already know that the content is wrapped to the correct width in
	// RawRender, so we can just apply the styles directly to each line.
	//
	// The split + per-line prefix loop is O(L); cache the result keyed
	// by (width, focused, sectionsFingerprint) so steady-state Render
	// becomes a pointer return. The sectionsFingerprint folds in the
	// per-section srcHash/extra so that any sub-cache change
	// invalidates this prefix cache without requiring an explicit
	// drop. Bypass the cache while spinning (RawRender's spinner
	// suffix changes every animation frame) or while a highlight
	// range is active (selection drag).
	useCache := !a.isSpinning() && !a.isHighlighted()
	cappedWidth := cappedMessageWidth(width)
	key := a.prefixCacheKey(cappedWidth)
	if useCache {
		if cached, ok := a.getCachedPrefixedRender(width, key); ok {
			return cached
		}
	}
	focused := a.sty.Messages.AssistantFocused.Render()
	blurred := a.sty.Messages.AssistantBlurred.Render()
	rendered := a.RawRender(width)
	lines := strings.Split(rendered, "\n")
	for i, line := range lines {
		switch {
		case i == a.bulletLine:
			// Each reply opens with a "•" in the gutter, like Codex.
			bullet := a.sty.Messages.AssistantBullet
			if a.focused {
				bullet = bullet.Foreground(a.sty.Messages.AssistantFocused.GetBorderLeftForeground())
			}
			lines[i] = bullet.Render("•") + " " + line
		case a.focused:
			lines[i] = focused + line
		default:
			lines[i] = blurred + line
		}
	}
	out := strings.Join(lines, "\n")
	if useCache {
		a.setCachedPrefixedRender(out, width, key)
	}
	return out
}

// prefixCacheKey builds the F3 prefixed-render cache key. We pack the
// focus bit into bit 0 and a fingerprint of the section caches into
// the upper bits, so any change to a sub-section's source text or
// extras forces the prefix cache to miss without needing an explicit
// drop. cappedWidth is included so a cached prefix never survives a
// section-cache miss caused by a width change. The finish reason is
// folded in too because it controls the composition of
// renderMessageContent (e.g. appending the constant "Canceled"
// string) — that decision lives outside any section's own hash.
func (a *AssistantMessageItem) prefixCacheKey(cappedWidth int) uint64 {
	thinkSrc, thinkExtra := a.thinkingKey()
	contentSrc, contentExtra := a.contentKey()
	errSrc, errExtra := a.errorKey()
	h := fnv.New64a()
	var buf [8]byte
	writeU64 := func(v uint64) {
		for i := range 8 {
			buf[i] = byte(v >> (8 * i))
		}
		_, _ = h.Write(buf[:])
	}
	writeU64(uint64(cappedWidth))
	writeU64(thinkSrc)
	writeU64(thinkExtra)
	writeU64(contentSrc)
	writeU64(contentExtra)
	writeU64(errSrc)
	writeU64(errExtra)
	writeU64(a.compositionKey())
	fingerprint := h.Sum64()
	var focusBit uint64
	if a.focused {
		focusBit = 1
	}
	return (fingerprint &^ 1) | focusBit
}

// compositionKey hashes the inputs to renderMessageContent's structural
// decisions (which sections to include, whether to append the
// constant "Canceled" footer) so that flipping IsFinished or the
// finish reason invalidates the prefix cache even when no section's
// own source text changed.
func (a *AssistantMessageItem) compositionKey() uint64 {
	var finishedFlag byte
	var reason string
	if a.message.IsFinished() {
		finishedFlag = 1
		reason = string(a.message.FinishReason())
	}
	// Length-prefixed framing keeps the finished flag and the reason
	// string from blending into one another.
	return fnvFields([]byte{finishedFlag}, []byte(reason))
}

// renderMessageContent renders the message content including thinking, main
// content, and finish reason. Each section is served from its own cache;
// only the section whose source text or extras changed since the last
// render is recomputed.
func (a *AssistantMessageItem) renderMessageContent(width int) (string, int) {
	var messageParts []string
	thinking := ""
	if !a.hideReasoning {
		thinking = strings.TrimSpace(a.message.ReasoningContent().Thinking)
	}
	content := strings.TrimSpace(a.message.Content().Text)

	if thinking != "" {
		messageParts = append(messageParts, a.cachedThinking(width))
	}

	a.bulletLine = -1
	if content != "" {
		if thinking != "" {
			messageParts = append(messageParts, "")
		}
		rendered := a.cachedContent(width)
		// Plan cards open with their own border, which is marker enough.
		if !common.PlanStartMarkerPresent(a.message.Content().Text) && !common.PlanReadyMarkerPresent(a.message.Content().Text) {
			start := lipgloss.Height(strings.Join(messageParts, "\n"))
			if len(messageParts) == 0 {
				start = 0
			}
			for i, line := range strings.Split(rendered, "\n") {
				if strings.TrimSpace(ansi.Strip(line)) != "" {
					a.bulletLine = start + i
					break
				}
			}
		}
		messageParts = append(messageParts, rendered)
	}

	if a.message.IsFinished() {
		var banner string
		switch {
		case a.message.FinishReason() == message.FinishReasonCanceled:
			// Tool calls render below this item, and cancelling a turn
			// closes each unfinished one out with its own interrupted
			// result. Saying it here too would put "Canceled" above the
			// tools it is describing.
			if len(a.message.ToolCalls()) > 0 {
				break
			}
			banner = a.sty.Messages.AssistantCanceled.Render("Canceled")
		case a.message.IsErrorLike():
			banner = a.cachedError(width)
		}
		if banner != "" {
			// Set the banner off from the text above it, or it reads as
			// the end of the sentence the model was in the middle of.
			if len(messageParts) > 0 {
				messageParts = append(messageParts, "")
			}
			messageParts = append(messageParts, banner)
		}
	}

	out := strings.Join(messageParts, "\n")
	return out, lipgloss.Height(out)
}

// thinkingKey returns the (srcHash, extra) cache key components for the
// thinking section. extra folds in everything other than the raw
// thinking text that affects the rendered output: the view mode
// (collapsed / tail-window / full) and the footer state (which
// depends on IsThinking, ToolCalls, and ThinkingDuration).
//
// The source hash is computed incrementally: during streaming the
// thinking text only grows by appending, so we continue the FNV-64a
// hash from the saved state rather than re-hashing the entire
// accumulated text. See CHARM-1785.
func (a *AssistantMessageItem) thinkingKey() (uint64, uint64) {
	if a.hideReasoning {
		return 0, 0
	}
	thinking := a.message.ReasoningContent().Thinking
	srcHash := a.thinkingHashIncremental(thinking)

	showFooter := !a.message.IsThinking() || len(a.message.ToolCalls()) > 0
	var durationStr string
	if showFooter {
		duration := a.message.ThinkingDuration()
		if duration.String() != "0s" {
			durationStr = duration.String()
		}
	}
	var footer byte
	if showFooter {
		footer = 1
	}
	// Length-prefixed framing avoids any delimiter collision between
	// the flag bytes and the duration string. The view mode is folded
	// in so that toggling collapsed ↔ tail-window ↔ full invalidates
	// only the thinking section, not content/error.
	extra := fnvFields([]byte{byte(a.thinkingViewMode), footer}, []byte(durationStr))
	return srcHash, extra
}

// thinkingHashIncremental returns the FNV-64a hash of thinking,
// continuing from the saved state when thinking is a prefix-extension
// of the previously hashed text. Falls back to a full re-hash when
// the text shrinks or diverges (e.g. user retried the turn).
func (a *AssistantMessageItem) thinkingHashIncremental(thinking string) uint64 {
	// Detect divergence: if the saved sample no longer matches the
	// start of the current text, the content was rewritten (retry)
	// and we must re-hash from scratch.
	sampleLen := min(len(thinking), 64)
	if a.thinkingHashLen > 0 && len(thinking) >= a.thinkingHashLen &&
		thinking[:sampleLen] == a.thinkingHashSample {
		// Fast path: continue hashing from saved state.
		h := a.thinkingHash
		for i := a.thinkingHashLen; i < len(thinking); i++ {
			h ^= uint64(thinking[i])
			h *= 1099511628211
		}
		a.thinkingHash = h
		a.thinkingHashLen = len(thinking)
		return h
	}
	// Full re-hash (first call, or text diverged/shrank).
	h := fnv64(thinking)
	a.thinkingHash = h
	a.thinkingHashLen = len(thinking)
	a.thinkingHashSample = thinking[:sampleLen]
	return h
}

// contentKey returns the (srcHash, extra) cache key components for the
// main content section. extra folds in the streaming-plan state so a
// message flips between the plain, open-card, and closed-card renders
// as the plan run progresses.
func (a *AssistantMessageItem) contentKey() (uint64, uint64) {
	var planStreaming byte
	if a.planStreaming() {
		planStreaming = 1
	}
	return fnv64(a.message.Content().Text), uint64(planStreaming)
}

// SetPlanAgent flags this item as plan-agent output (or clears the
// flag). The flag scopes plan-card rendering to plan mode; the plan
// start marker decides which message is the plan.
// SetSuperseded sets whether something newer follows this step in the chat.
func (a *AssistantMessageItem) SetSuperseded(superseded bool) {
	if a.superseded != superseded {
		a.superseded = superseded
		a.Bump()
	}
}

// SetCompactingEarlier sets whether an earlier, superseded step is
// compacting the conversation.
func (a *AssistantMessageItem) SetCompactingEarlier(compacting bool) {
	if a.compactingEarlier != compacting {
		a.compactingEarlier = compacting
		a.Bump()
	}
}

// Compacting reports whether this unfinished step is compacting the
// conversation.
func (a *AssistantMessageItem) Compacting() bool {
	return a.message.IsCompacting && !a.message.IsFinished()
}

func (a *AssistantMessageItem) SetPlanAgent(plan bool) {
	if a.planAgent == plan {
		return
	}
	a.planAgent = plan
	a.Bump()
}

// planStreaming reports whether the plan is still streaming into
// this message: the item belongs to the plan agent and the message
// has not finished. The bottom border is withheld until the
// plan-ready marker lands.
func (a *AssistantMessageItem) planStreaming() bool {
	return a.planAgent && !a.message.IsFinished()
}

// errorKey returns the (srcHash, extra) cache key components for the
// error / refusal section. Returns (0, 0) when no error-like finish
// is present so the cache stays a no-op for normal messages.
func (a *AssistantMessageItem) errorKey() (uint64, uint64) {
	if !a.message.IsFinished() || !a.message.IsErrorLike() {
		return 0, 0
	}
	finishPart := a.message.FinishPart()
	if finishPart == nil {
		return 0, 0
	}
	// Length-prefixed framing prevents Message+Details collisions
	// between distinct (Message, Details) tuples that would
	// otherwise concatenate to the same byte sequence. Fold the
	// reason in so ERROR vs REFUSED banners never share a cache slot.
	return fnvFields([]byte(finishPart.Reason), []byte(finishPart.Message), []byte(finishPart.Details)), 0
}

// cachedThinking returns the rendered thinking section, computing and
// caching it on miss. The thinking-box height (used for click target
// detection) is preserved across hits via assistantSection.aux so the
// cached path never desyncs click detection.
func (a *AssistantMessageItem) cachedThinking(width int) string {
	srcHash, extra := a.thinkingKey()
	if a.thinkingSec.hit(width, srcHash, extra) {
		a.thinkingBoxHeight = a.thinkingSec.aux
		return a.thinkingSec.out
	}
	out := a.renderThinking(a.message.ReasoningContent().Thinking, width)
	a.thinkingSec.store(width, srcHash, extra, out, a.thinkingBoxHeight)
	return out
}

// cachedContent returns the rendered content section.
func (a *AssistantMessageItem) cachedContent(width int) string {
	srcHash, extra := a.contentKey()
	if a.contentSec.hit(width, srcHash, extra) {
		return a.contentSec.out
	}
	text := a.message.Content().Text
	// In plan mode the agent ends its final plan with a sentinel marker.
	// Hide the end plan marker and wrap the message in a background "card" so
	// the plan stands out from regular assistant replies. Mirrors the
	// ThinkingBox treatment.
	var out string
	switch {
	case common.PlanReadyMarkerPresent(text):
		out = a.renderPlanCard(common.StripPlanMarkers(text), width)
	case a.planStreaming() && common.PlanStartMarkerPresent(text):
		// While the plan streams, draw the card as an open box: top
		// and side borders only. The bottom border closes once the
		// plan-ready marker arrives. The plan-start marker gates the
		// card so intermediate exploratory replies in plan mode never
		// grow a border.
		out = a.renderPlanCardStreaming(common.StripPlanMarkers(text), width)
	default:
		out = a.renderMarkdown(common.StripPlanMarkers(text), width)
	}
	a.contentSec.store(width, srcHash, extra, out, 0)
	return out
}

// isSpinning returns true if the assistant message is still generating.
func (a *AssistantMessageItem) isSpinning() bool {
	// Commentary can be followed by a long wait for the next provider event.
	// Keep status visible until the step finishes or hands off to tools,
	// whose own status takes over.
	return !a.superseded && !a.message.IsFinished() && len(a.message.ToolCalls()) == 0
}

// SetMessage is used to update the underlying message. Only the
// sub-section caches whose source text or extras changed are
// invalidated; the others survive and serve cache hits on the next
// RawRender.
func (a *AssistantMessageItem) SetMessage(msg *message.Message) {
	a.message = msg
	a.syncActivity(time.Now())
	// Bump the F6 version even if the underlying *message.Message
	// pointer is identical: callers may have mutated the message in
	// place (delta append) and we cannot tell from here. The
	// per-section caches dedupe identical content via FNV-64 hashes,
	// so a redundant bump only costs one list-cache repopulation.
	a.Bump()
	// The prefix cache is keyed by a fingerprint that includes every
	// section's source hash, so an unchanged section keeps its prefix
	// cache valid while a changed section forces a miss naturally.
	// Section caches themselves are content-keyed, so they do not
	// need an explicit drop here either. If the message started
	// spinning the UI's animation clock picks it up on the next
	// update.
}

// Cancel shows a cancellation before the agent's terminal event arrives.
func (a *AssistantMessageItem) Canceled() bool {
	return a.message.FinishReason() == message.FinishReasonCanceled
}

// Cancel shows a cancellation before the agent's terminal event arrives.
func (a *AssistantMessageItem) Cancel() string {
	if a.message.IsFinished() {
		return ""
	}
	msg := a.message.Clone()
	msg.AddFinish(message.FinishReasonCanceled, "", "")
	a.SetMessage(&msg)
	return msg.ID
}

// Finished implements list.Item. The assistant message is freezable
// once the message reports IsFinished() and is no longer spinning
// (no animation frame remains pending). Streaming tail animation is
// caught by isSpinning, so freezing only kicks in once the turn is
// fully terminal. The list cache invalidates the entry on the next
// version bump if anything (focus, highlight, expansion) changes.
func (a *AssistantMessageItem) Finished() bool {
	return a.message.IsFinished() && !a.isSpinning()
}

// clearCache drops every cached render for this item, including the
// per-section caches. Shadows the embedded cachedMessageItem.clearCache
// so ClearItemCaches (style change) wipes the section caches too.
// F8: also drop the streaming-markdown stable-prefix cache because
// the cached glamour render embeds the OLD style's ANSI sequences
// and is no longer visually consistent with the new style.
func (a *AssistantMessageItem) clearCache() {
	a.cachedMessageItem.clearCache()
	a.thinkingSec.reset()
	a.contentSec.reset()
	a.errorSec.reset()
	a.streamingContent.Reset()
	a.streamingThinking.Reset()
	a.streamingPlan.Reset()
	a.thinkingHash = 0
	a.thinkingHashLen = 0
	a.thinkingHashSample = ""
}

// ToggleExpanded advances the F5 thinking view-mode cycle and returns
// whether the item is now in any expanded state (tail-window or full).
// The cycle is collapsed → tail-window → full → collapsed, with the
// tail-window step skipped when the rendered thinking fits within
// maxExpandedThinkingTailLines so short blocks remain a two-click
// toggle. Both the thinking section cache and the F3 prefix cache
// fold thinkingViewMode into their keys, so no explicit invalidation
// is required here.
//
// When the message carries no thinking text the toggle is a no-op:
// there is nothing to expand, and mutating the view mode would
// thrash the thinking-section cache key for no visible benefit.
func (a *AssistantMessageItem) ToggleExpanded() bool {
	if a.hideReasoning {
		return false
	}
	if strings.TrimSpace(a.message.ReasoningContent().Thinking) == "" {
		return a.thinkingViewMode != thinkingCollapsed
	}
	switch a.thinkingViewMode {
	case thinkingCollapsed:
		if a.tailWindowWouldTruncate() {
			a.thinkingViewMode = thinkingTailWindow
		} else {
			a.thinkingViewMode = thinkingFullExpanded
		}
	case thinkingTailWindow:
		a.thinkingViewMode = thinkingFullExpanded
	case thinkingFullExpanded:
		a.thinkingViewMode = thinkingCollapsed
	}
	// View-mode changes alter the windowing slice applied after
	// glamour render. The streaming prefix cache may have been
	// seeded under a different slice regime, and glued renders are
	// not byte-identical to monolithic ones. Drop the prefix cache
	// so the next render is clean.
	a.streamingThinking.Reset()
	a.Bump()
	return a.thinkingViewMode != thinkingCollapsed
}

// tailWindowWouldTruncate reports whether the current thinking text
// is long enough that the tail-window step is worth inserting into
// the toggle cycle. We use a cheap source-text logical-line count
// as the heuristic rather than peeking into the cache: the cache
// may be populated in collapsed state (where its height is bounded
// by maxCollapsedThinkingHeight and tells us nothing about the
// underlying length), and re-running glamour just to count lines
// would defeat the cache. The heuristic can over-trigger (a source
// with many short lines may wrap to fewer than N lines), in which
// case the tail-window render is visually identical to full and
// the cycle costs the user one extra toggle — preferred over the
// alternative of failing to show the affordance on a genuinely
// long block.
//
// Logical line count is `1 + newlineCount` (a string with no
// newlines is one line). Comparing newline count alone introduced
// an off-by-one that let a source whose post-newline-split length
// equalled the cap skip the tail-window step.
func (a *AssistantMessageItem) tailWindowWouldTruncate() bool {
	lineCount := 1 + strings.Count(a.message.ReasoningContent().Thinking, "\n")
	return lineCount > maxExpandedThinkingTailLines
}

// HandleMouseClick implements MouseClickable. It signals (via a true return)
// that the click lies on the thinking box so the caller can invoke
// [AssistantMessageItem.ToggleExpanded] through the generic [Expandable]
// path. Toggling here directly would double-toggle because the caller always
// runs the generic path after a handled click.
func (a *AssistantMessageItem) HandleMouseClick(btn ansi.MouseButton, x, y int) bool {
	if a.hideReasoning || btn != ansi.MouseLeft {
		return false
	}
	// Only the thinking box is clickable; other regions of the assistant
	// message should not trigger expansion.
	return a.thinkingBoxHeight > 0 && y < a.thinkingBoxHeight
}

// HandleKeyEvent implements KeyEventHandler.
func (a *AssistantMessageItem) HandleKeyEvent(key tea.KeyMsg) (bool, tea.Cmd) {
	if k := key.String(); k == "c" || k == "y" {
		text := a.CopyText()
		return true, common.CopyToClipboard(text, "Message copied to clipboard")
	}
	return false, nil
}

// onlyThinking reports whether this step shows nothing but thinking, so
// it can fold into a tool status group.
func (a *AssistantMessageItem) onlyThinking() bool {
	m := a.message
	// A cancelled step that called tools shows no "Canceled" banner (its
	// tools report the interruption), so it has nothing of its own to show
	// either; standing alone it would leave a blank row.
	canceledBanner := m.FinishReason() == message.FinishReasonCanceled && len(m.ToolCalls()) == 0
	return strings.TrimSpace(m.Content().Text) == "" && !m.IsErrorLike() &&
		!canceledBanner && !m.IsSummaryMessage && !m.IsCompacting && !a.planAgent
}
