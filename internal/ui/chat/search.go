package chat

import (
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/fsext"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
)

// -----------------------------------------------------------------------------
// Glob Tool
// -----------------------------------------------------------------------------

// GlobToolMessageItem is a message item that represents a glob tool call.
type GlobToolMessageItem struct {
	*baseToolMessageItem
}

var _ ToolMessageItem = (*GlobToolMessageItem)(nil)

// NewGlobToolMessageItem creates a new [GlobToolMessageItem].
func NewGlobToolMessageItem(
	sty *styles.Styles,
	toolCall message.ToolCall,
	result *message.ToolResult,
	canceled bool,
) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &GlobToolRenderContext{}, canceled)
}

// GlobToolRenderContext renders glob tool messages.
type GlobToolRenderContext struct{}

// RenderTool implements the [ToolRenderer] interface.
func (g *GlobToolRenderContext) RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string {
	return globTool.render(sty, width, opts)
}

// globTool draws Glob tool calls with the standard layout.
var globTool = standardTool[tools.GlobParams]{
	name: "Glob",
	header: func(params tools.GlobParams) []string {
		toolParams := []string{params.Pattern}
		if params.Path != "" {
			toolParams = append(toolParams, "path", params.Path)
		}
		return toolParams
	},
	body: plainResultBody[tools.GlobParams],
}

// -----------------------------------------------------------------------------
// Grep Tool
// -----------------------------------------------------------------------------

// GrepToolMessageItem is a message item that represents a grep tool call.
type GrepToolMessageItem struct {
	*baseToolMessageItem
}

var _ ToolMessageItem = (*GrepToolMessageItem)(nil)

// NewGrepToolMessageItem creates a new [GrepToolMessageItem].
func NewGrepToolMessageItem(
	sty *styles.Styles,
	toolCall message.ToolCall,
	result *message.ToolResult,
	canceled bool,
) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &GrepToolRenderContext{}, canceled)
}

// GrepToolRenderContext renders grep tool messages.
type GrepToolRenderContext struct{}

// RenderTool implements the [ToolRenderer] interface.
func (g *GrepToolRenderContext) RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string {
	return grepTool.render(sty, width, opts)
}

// grepTool draws Grep tool calls with the standard layout.
var grepTool = standardTool[tools.GrepParams]{
	name: "Grep",
	header: func(params tools.GrepParams) []string {
		toolParams := []string{params.Pattern}
		if params.Path != "" {
			toolParams = append(toolParams, "path", params.Path)
		}
		if params.Include != "" {
			toolParams = append(toolParams, "include", params.Include)
		}
		if params.LiteralText {
			toolParams = append(toolParams, "literal", "true")
		}
		return toolParams
	},
	body: plainResultBody[tools.GrepParams],
}

// -----------------------------------------------------------------------------
// LS Tool
// -----------------------------------------------------------------------------

// LSToolMessageItem is a message item that represents an ls tool call.
type LSToolMessageItem struct {
	*baseToolMessageItem
}

var _ ToolMessageItem = (*LSToolMessageItem)(nil)

// NewLSToolMessageItem creates a new [LSToolMessageItem].
func NewLSToolMessageItem(
	sty *styles.Styles,
	toolCall message.ToolCall,
	result *message.ToolResult,
	canceled bool,
) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &LSToolRenderContext{}, canceled)
}

// LSToolRenderContext renders ls tool messages.
type LSToolRenderContext struct{}

// RenderTool implements the [ToolRenderer] interface.
func (l *LSToolRenderContext) RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string {
	return lsTool.render(sty, width, opts)
}

// lsTool draws List tool calls with the standard layout.
var lsTool = standardTool[tools.LSParams]{
	name: "List",
	header: func(params tools.LSParams) []string {
		path := params.Path
		if path == "" {
			path = "."
		}
		path = fsext.PrettyPath(path)
		return []string{path}
	},
	body: plainResultBody[tools.LSParams],
}

// -----------------------------------------------------------------------------
// Sourcegraph Tool
// -----------------------------------------------------------------------------

// SourcegraphToolMessageItem is a message item that represents a sourcegraph tool call.
type SourcegraphToolMessageItem struct {
	*baseToolMessageItem
}

var _ ToolMessageItem = (*SourcegraphToolMessageItem)(nil)

// NewSourcegraphToolMessageItem creates a new [SourcegraphToolMessageItem].
func NewSourcegraphToolMessageItem(
	sty *styles.Styles,
	toolCall message.ToolCall,
	result *message.ToolResult,
	canceled bool,
) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &SourcegraphToolRenderContext{}, canceled)
}

// SourcegraphToolRenderContext renders sourcegraph tool messages.
type SourcegraphToolRenderContext struct{}

// RenderTool implements the [ToolRenderer] interface.
func (s *SourcegraphToolRenderContext) RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string {
	return sourcegraphTool.render(sty, width, opts)
}

// sourcegraphTool draws Sourcegraph tool calls with the standard layout.
var sourcegraphTool = standardTool[tools.SourcegraphParams]{
	name: "Sourcegraph",
	header: func(params tools.SourcegraphParams) []string {
		toolParams := []string{params.Query}
		if params.Count != 0 {
			toolParams = append(toolParams, "count", formatNonZero(params.Count))
		}
		if params.ContextWindow != 0 {
			toolParams = append(toolParams, "context", formatNonZero(params.ContextWindow))
		}
		return toolParams
	},
	body: plainResultBody[tools.SourcegraphParams],
}
