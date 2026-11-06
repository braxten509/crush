package chat

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/anim"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
)

// pendingTool renders a tool that is still in progress with an animation.
func pendingTool(sty *styles.Styles, name string, anim *anim.Anim, nested bool) string {
	icon := sty.Tool.IconPending.Render()
	nameStyle := sty.Tool.NameNormal
	if nested {
		nameStyle = sty.Tool.NameNested
	}
	toolName := nameStyle.Render(name)

	var animView string
	if anim != nil {
		animView = anim.Render()
	}

	return fmt.Sprintf("%s %s %s", icon, toolName, animView)
}

// toolEarlyStateContent handles error/cancelled/pending states before content rendering.
// Returns the rendered output and true if early state was handled.
func toolEarlyStateContent(sty *styles.Styles, opts *ToolRenderOpts, width int) (string, bool) {
	var msg string
	switch opts.Status {
	case ToolStatusError:
		msg = toolErrorContent(sty, opts.Result, width)
	case ToolStatusCanceled:
		msg = sty.Tool.StateCancelled.Render("Canceled.")
	case ToolStatusAwaitingPermission:
		msg = sty.Tool.StateWaiting.Render("Requesting permission...")
	case ToolStatusRunning:
		msg = sty.Tool.StateWaiting.Render("Waiting for tool response...")
	default:
		return "", false
	}
	return msg, true
}

// toolErrorContent formats an error message with an ERROR or WARN tag.
func toolErrorContent(sty *styles.Styles, result *message.ToolResult, width int) string {
	if result == nil {
		return ""
	}
	errContent := strings.ReplaceAll(result.Content, "\n", " ")
	if strings.Contains(errContent, "User denied permission") ||
		strings.Contains(errContent, "User cancelled") {
		deniedTag := sty.Tool.WarnTag.Render("WARN")
		deniedTagWidth := lipgloss.Width(deniedTag)
		errContent = ansi.Truncate(errContent, width-deniedTagWidth-3, "…")
		return fmt.Sprintf("%s %s", deniedTag, sty.Tool.WarnMessage.Render(errContent))
	}
	errTag := sty.Tool.ErrorTag.Render("ERROR")
	tagWidth := lipgloss.Width(errTag)
	errContent = ansi.Truncate(errContent, width-tagWidth-3, "…")
	return fmt.Sprintf("%s %s", errTag, sty.Tool.ErrorMessage.Render(errContent))
}

// toolIcon returns the status icon for a tool call.
// toolIcon returns the status icon for a tool call based on its status.
func toolIcon(sty *styles.Styles, status ToolStatus) string {
	switch status {
	case ToolStatusSuccess:
		return sty.Tool.IconSuccess.String()
	case ToolStatusError:
		return sty.Tool.IconError.String()
	case ToolStatusCanceled:
		return sty.Tool.IconCancelled.String()
	default:
		return sty.Tool.IconPending.String()
	}
}

// toolParamList formats tool parameters as "main (key=value, ...)" with truncation.
// When opts.ExpandedContent is true, the output wraps instead of truncating,
// unless the tool is compact (nested in an agent tree), which always truncates.
func toolParamList(sty *styles.Styles, params []string, width int, opts *ToolRenderOpts) string {
	// minSpaceForMainParam is the min space required for the main param
	// if this is less that the value set we will only show the main param nothing else
	const minSpaceForMainParam = 30
	if len(params) == 0 {
		return ""
	}

	mainParam := params[0]

	// Build key=value pairs from remaining params (consecutive key, value pairs).
	var kvPairs []string
	for i := 1; i+1 < len(params); i += 2 {
		if params[i+1] != "" {
			kvPairs = append(kvPairs, fmt.Sprintf("%s=%s", params[i], params[i+1]))
		}
	}

	// Try to include key=value pairs if there's enough space.
	output := mainParam
	if len(kvPairs) > 0 {
		partsStr := strings.Join(kvPairs, ", ")
		if remaining := width - lipgloss.Width(partsStr) - 3; remaining >= minSpaceForMainParam {
			// main param is syntax highlighted so style the param as well
			pairs := sty.Tool.ParamMain.Render(fmt.Sprintf("(%s)", partsStr))
			output = fmt.Sprintf("%s %s", mainParam, pairs)
		}
	}

	// Compact tools are the children of an agent tree, where a wrapped
	// header would push continuation lines past the tree's branch drawing
	// and break the bars. They truncate even when expanded, and stay on one
	// line even when a parameter has newlines of its own.
	if opts != nil && opts.Compact {
		output = strings.Join(strings.Fields(output), " ")
	}
	if width >= 0 && (opts == nil || !opts.ExpandedContent || opts.Compact) {
		return sty.Tool.ParamMain.Render(ansi.Truncate(output, width, "…"))
	}
	if opts != nil && opts.ExpandedContent && width > 0 && lipgloss.Width(output) > width {
		// Let Lip Gloss do the wrapping rather than wrapping and styling in
		// separate steps. A parameter is often syntax highlighted, and a
		// wrap falls wherever the width runs out, usually part-way through a
		// coloured token. Lip Gloss carries the colours that are open at the
		// break and re-opens them on the next line, on top of the parameter
		// style. Hard-wrapping first and styling after would emit only the
		// parameter colour there and drop the highlight; styling first and
		// hard-wrapping after would emit nothing there and drop both.
		return capLineWidths(sty.Tool.ParamMain.Width(width).Render(output), width)
	}
	return sty.Tool.ParamMain.Render(output)
}

// capLineWidths holds every line of s to width.
//
// Word wrapping keeps the space it broke on, so a word that ends exactly at
// the edge leaves its line a column wider than the space it was given, and
// the header spills past the pane. The space is invisible and sits before
// the closing reset, out of reach of a plain trim, so the line is cut with
// the escape sequences accounted for.
func capLineWidths(s string, width int) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if lipgloss.Width(line) > width {
			lines[i] = ansi.Truncate(line, width, "")
		}
	}
	return strings.Join(lines, "\n")
}

// toolHeader builds the tool header line: "● ToolName params..."
// When opts.ExpandedContent is true, long parameters wrap instead of truncating.
func toolHeader(sty *styles.Styles, status ToolStatus, name string, width int, opts *ToolRenderOpts, params ...string) string {
	prefix := toolHeaderPrefix(sty, status, name, opts)
	prefixWidth := lipgloss.Width(prefix)
	paramsStr := toolParamList(sty, params, width-prefixWidth, opts)

	// When expanded, toolParamList may return multiple lines. Indent
	// continuation lines to align with the first line's param text.
	if first, rest, found := strings.Cut(paramsStr, "\n"); found {
		return prefix + first + "\n" + indentLines(rest, prefixWidth)
	}
	return prefix + paramsStr
}

// toolHeaderPrefix returns the styled "● ToolName " that leads a tool header.
func toolHeaderPrefix(sty *styles.Styles, status ToolStatus, name string, opts *ToolRenderOpts) string {
	nameStyle := sty.Tool.NameNormal
	if opts != nil && opts.Compact {
		nameStyle = sty.Tool.NameNested
	}
	return fmt.Sprintf("%s %s ", toolIcon(sty, status), nameStyle.Render(name))
}

// indentLines indents every line of s by n spaces.
func indentLines(s string, n int) string {
	indent := strings.Repeat(" ", n)
	return indent + strings.ReplaceAll(s, "\n", "\n"+indent)
}
