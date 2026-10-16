package chat

import (
	"cmp"
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
)

// renderPlanCard renders the final plan message as a full-width bordered
// card. The markdown is rendered at the card's inner width (accounting for
// PlanBox's horizontal frame) with the PlanMarkdown style; PlanBox then
// draws the border and padding and pads each line out to full width. The
// plan is final by the time the marker appears, so this bypasses the
// streaming-markdown cache and renders directly, like renderThinking.
func (a *AssistantMessageItem) renderPlanCard(text string, width int) string {
	box, innerWidth := planBoxLayout(a.sty.Messages.PlanBox, width)
	renderer := common.PlanMarkdownRenderer(a.sty, innerWidth)
	mu := common.LockMarkdownRenderer(renderer)
	mu.Lock()
	rendered, err := renderer.Render(text)
	mu.Unlock()
	if err != nil {
		rendered = text
	}
	return renderPlanBox(box, rendered, width, true)
}

// renderPlanCardStreaming renders the plan while it is still streaming
// as an open card: top and side borders only, no bottom border. It
// routes through the stable-prefix streaming cache so each streaming
// flush only re-renders the trailing partial of the plan document.
func (a *AssistantMessageItem) renderPlanCardStreaming(text string, width int) string {
	box, innerWidth := planBoxLayout(a.sty.Messages.PlanBox, width)
	renderer := common.PlanMarkdownRenderer(a.sty, innerWidth)
	rendered := a.streamingPlan.Render(text, innerWidth, renderer)
	return renderPlanBox(box, rendered, width, false)
}

// renderPlanBox applies the card layout: an un-filled bordered box whose
// padding lets the terminal background show through. Only intentional
// backgrounds (the inline-code chip, the H1 badge) keep a color of their
// own; everything else renders on the terminal background. With closed
// false the bottom border is withheld, leaving the card open while the
// plan streams.
func renderPlanBox(style lipgloss.Style, content string, width int, closed bool) string {
	style, innerWidth := planBoxLayout(style, width)
	if !closed {
		style = style.BorderBottom(false)
	}
	lines := strings.Split(strings.TrimSpace(content), "\n")
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, innerWidth, "")
	}
	// In lipgloss v2 Width is the total box width: border and padding live
	// inside it, so the content area ends up Width minus the frame. Glamour
	// already wrapped at innerWidth (width minus the frame), so size the box
	// to the full width or every line gets re-wrapped narrower.
	return style.Width(max(1, width)).Render(strings.Join(lines, "\n"))
}

// planBoxLayout returns a style and content width whose combined horizontal
// frame fits within the available message width.
func planBoxLayout(style lipgloss.Style, width int) (lipgloss.Style, int) {
	width = max(1, width)
	frameWidth := style.GetHorizontalFrameSize()
	if frameWidth >= width {
		style = style.PaddingLeft(0).PaddingRight(0)
		frameWidth = style.GetHorizontalFrameSize()
	}
	return style, max(1, width-frameWidth)
}

// cachedError returns the rendered error section.
func (a *AssistantMessageItem) cachedError(width int) string {
	srcHash, extra := a.errorKey()
	if a.errorSec.hit(width, srcHash, extra) {
		return a.errorSec.out
	}
	out := a.renderError(width)
	a.errorSec.store(width, srcHash, extra, out, 0)
	return out
}

// renderThinking renders the thinking/reasoning content with footer.
//
// Slicing happens AFTER glamour rendering so fenced code blocks, list
// continuations, and tables are not split mid-block — the same
// boundary problem §4.4 of the design note flags. The bordered
// ThinkingBox style is applied on top of the (already-windowed)
// lines so the visual box matches what the user sees today.
func (a *AssistantMessageItem) renderThinking(thinking string, width int) string {
	renderer := common.QuietMarkdownRenderer(a.sty, width)
	rendered := a.streamingThinking.Render(thinking, width, renderer)
	rendered = strings.TrimSpace(rendered)
	// The renderer already knows this count from its cached prefix, so
	// take it rather than rescanning a document that only grows.
	renderedLines := a.streamingThinking.LastLines()

	// Count lines and, for the windowed view modes, slice the tail
	// WITHOUT splitting the entire rendered document. Splitting a
	// 1200-line render just to keep the last 10 lines is O(n) per
	// tick; tailLines finds the cut point with a bounded backward
	// scan. See CHARM-1785.
	var lines []string
	var totalLines int
	switch a.thinkingViewMode {
	case thinkingCollapsed:
		totalLines = renderedLines
		// Avoid hiding a single line; showing it beats the hint.
		if totalLines > maxCollapsedThinkingHeight+1 {
			tail, hidden := tailLines(rendered, maxCollapsedThinkingHeight, totalLines)
			hint := a.sty.Messages.ThinkingTruncationHint.Render(
				fmt.Sprintf(assistantMessageTruncateFormat, hidden),
			)
			lines = append([]string{hint, ""}, strings.Split(tail, "\n")...)
		} else {
			lines = strings.Split(rendered, "\n")
		}
	case thinkingTailWindow:
		totalLines = renderedLines
		if totalLines > maxExpandedThinkingTailLines+1 {
			tail, hidden := tailLines(rendered, maxExpandedThinkingTailLines, totalLines)
			hint := a.sty.Messages.ThinkingTruncationHint.Render(
				fmt.Sprintf(assistantMessageTailWindowFormat, hidden),
			)
			lines = append([]string{hint, ""}, strings.Split(tail, "\n")...)
		} else {
			lines = strings.Split(rendered, "\n")
		}
	default:
		lines = strings.Split(rendered, "\n")
	}

	thinkingStyle := a.sty.Messages.ThinkingBox.Width(width)
	result := thinkingStyle.Render(strings.Join(lines, "\n"))
	a.thinkingBoxHeight = lipgloss.Height(result)

	var footer string
	// if thinking is done add the thought for footer
	if !a.message.IsThinking() || len(a.message.ToolCalls()) > 0 {
		duration := a.message.ThinkingDuration()
		if duration.String() != "0s" {
			footer = a.sty.Messages.ThinkingFooterTitle.Render("Thought for ") +
				a.sty.Messages.ThinkingFooterDuration.Render(duration.String())
		}
	}

	if footer != "" {
		result += "\n\n" + footer
	}

	return result
}

// renderMarkdown renders content as markdown. F8 routes the call
// through streamingContent, which caches the glamour render of a
// "stable prefix" so each streaming flush only re-renders the
// trailing partial. The streaming cache invalidates itself on
// width change and on any content that is not a prefix-extension
// of the previously rendered content (e.g. user retried the
// turn), and falls back to a full render whenever boundary
// detection has the slightest doubt — see
// findSafeMarkdownBoundary.
func (a *AssistantMessageItem) renderMarkdown(content string, width int) string {
	renderer := common.MarkdownRenderer(a.sty, width)
	return a.streamingContent.Render(content, width, renderer)
}

func (a *AssistantMessageItem) activityStatus(now time.Time) string {
	if !a.isSpinning() {
		return ""
	}
	if a.message.IsSummaryMessage || a.message.IsCompacting || a.compactingEarlier {
		return "Compacting conversation"
	}
	if status := retryActivityStatus(a.message.Activity); status != "" {
		return status
	}
	if a.message.Activity == "writing" {
		return "Writing command"
	}
	if a.message.Activity == "thinking" || a.message.IsThinking() {
		return "Thinking"
	}
	if a.message.Activity == "responding" && now.Sub(time.Unix(a.message.ActivityAt, 0)) < 5*time.Second {
		return "Responding"
	}
	return "Waiting for the model"
}

func retryActivityStatus(activity string) string {
	switch activity {
	case "rate_limited":
		return "Rate limited — waiting to retry"
	case "retrying":
		return "Waiting to retry"
	default:
		return ""
	}
}

func (a *AssistantMessageItem) syncActivity(now time.Time) string {
	status := a.activityStatus(now)
	a.activity.update(status, now)
	return status
}

func (a *AssistantMessageItem) renderSpinning() string {
	now := time.Now()
	label := a.syncActivity(now)
	a.anim.SetLabel(label)
	if label == "Compacting conversation" {
		return renderCompactingStatus(a.sty, a.activity.elapsed(now), a.anim.RenderEllipsis())
	}
	return a.anim.Render()
}

func renderCompactingStatus(sty *styles.Styles, elapsed, dots string) string {
	label := lipgloss.NewStyle().Foreground(sty.WorkingLabelColor).Render("Compacting conversation") + dots
	if elapsed != "" {
		label += " " + lipgloss.NewStyle().Foreground(sty.WorkingTimerColor).Render(elapsed)
	}
	return label
}

// renderError renders an error or provider-refusal banner.
func (a *AssistantMessageItem) renderError(width int) string {
	finishPart := a.message.FinishPart()
	tagLabel := "ERROR"
	titleText := finishPart.Message
	detailsText := finishPart.Details
	if finishPart.Reason == message.FinishReasonContentFilter {
		tagLabel = refusalTagLabel
		titleText = cmp.Or(titleText, refusalTitle)
		detailsText = cmp.Or(detailsText, refusalDetails)
	}
	errTag := a.sty.Messages.ErrorTag.Render(tagLabel)
	truncated := ansi.Truncate(titleText, width-2-lipgloss.Width(errTag), "...")
	title := fmt.Sprintf("%s %s", errTag, a.sty.Messages.ErrorTitle.Render(truncated))
	if detailsText == "" {
		return title
	}
	details := a.sty.Messages.ErrorDetails.Width(width - 2).Render(detailsText)
	return fmt.Sprintf("%s\n\n%s", title, details)
}
