package chat

import (
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
)

// SymbolsToolMessageItem is a message item that represents a symbols tool call.
type SymbolsToolMessageItem struct {
	*baseToolMessageItem
}

var _ ToolMessageItem = (*SymbolsToolMessageItem)(nil)

// NewSymbolsToolMessageItem creates a new [SymbolsToolMessageItem].
func NewSymbolsToolMessageItem(
	sty *styles.Styles,
	toolCall message.ToolCall,
	result *message.ToolResult,
	canceled bool,
) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &SymbolsToolRenderContext{}, canceled)
}

// SymbolsToolRenderContext renders symbols tool messages.
type SymbolsToolRenderContext struct{}

// RenderTool implements the [ToolRenderer] interface.
func (r *SymbolsToolRenderContext) RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string {
	return symbolsTool.render(sty, width, opts)
}

// symbolsTool draws List Symbols tool calls with the standard layout.
var symbolsTool = standardTool[tools.SymbolsParams]{
	name:    "List Symbols",
	lenient: true,
	header: func(params tools.SymbolsParams) []string {
		return []string{params.FilePath}
	},
	body: func(sty *styles.Styles, opts *ToolRenderOpts, params tools.SymbolsParams, cappedWidth int) string {
		// Render as code to preserve tree indentation.
		return toolOutputCodeContent(sty, params.FilePath, opts.Result.Content, 0, cappedWidth, opts.ExpandedContent)
	},
}
