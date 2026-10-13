package chat

import (
	"encoding/json"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
)

// standardTool describes a tool drawn with the common layout: a spinner
// while pending, a header listing the decoded parameters, the early error,
// cancel and wait states, and a body for a non-empty result.
type standardTool[P any] struct {
	// name is the tool name shown in the header.
	name string
	// lenient draws the tool with whatever parameters decoded when the
	// input is invalid, instead of reporting invalid parameters.
	lenient bool
	// header returns the header parameters: the main one, then key and
	// value pairs.
	header func(params P) []string
	// body renders a non-empty result at the capped width.
	body func(sty *styles.Styles, opts *ToolRenderOpts, params P, cappedWidth int) string
}

// render draws the tool at width.
func (s standardTool[P]) render(sty *styles.Styles, width int, opts *ToolRenderOpts) string {
	cappedWidth := cappedMessageWidth(width)
	if opts.IsPending() {
		return pendingTool(sty, s.name, opts.Anim, opts.Compact)
	}

	var params P
	if err := json.Unmarshal([]byte(opts.ToolCall.Input), &params); err != nil && !s.lenient {
		return toolErrorContent(sty, &message.ToolResult{Content: "Invalid parameters"}, cappedWidth)
	}

	header := toolHeader(sty, opts.Status, s.name, cappedWidth, opts, s.header(params)...)
	if opts.Compact {
		return header
	}

	if earlyState, ok := toolEarlyStateContent(sty, opts, cappedWidth); ok {
		return joinToolParts(header, earlyState)
	}

	if opts.HasEmptyResult() {
		return header
	}

	return joinToolParts(header, s.body(sty, opts, params, cappedWidth))
}

// plainResultBody renders the result content as plain text inside the
// tool body.
func plainResultBody[P any](sty *styles.Styles, opts *ToolRenderOpts, _ P, cappedWidth int) string {
	bodyWidth := cappedWidth - toolBodyLeftPaddingTotal
	return sty.Tool.Body.Render(toolOutputPlainContent(sty, opts.Result.Content, bodyWidth, opts.ExpandedContent))
}
