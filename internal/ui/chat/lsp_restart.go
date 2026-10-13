package chat

import (
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
)

// LSPRestartToolMessageItem is a message item that represents a lsprestart tool call.
type LSPRestartToolMessageItem struct {
	*baseToolMessageItem
}

var _ ToolMessageItem = (*LSPRestartToolMessageItem)(nil)

// NewLSPRestartToolMessageItem creates a new [LSPRestartToolMessageItem].
func NewLSPRestartToolMessageItem(
	sty *styles.Styles,
	toolCall message.ToolCall,
	result *message.ToolResult,
	canceled bool,
) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &LSPRestartToolRenderContext{}, canceled)
}

// LSPRestartToolRenderContext renders lsprestart tool messages.
type LSPRestartToolRenderContext struct{}

// RenderTool implements the [ToolRenderer] interface.
func (r *LSPRestartToolRenderContext) RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string {
	return lspRestartTool.render(sty, width, opts)
}

// lspRestartTool draws Restart LSP tool calls with the standard layout.
var lspRestartTool = standardTool[tools.LSPRestartParams]{
	name:    "Restart LSP",
	lenient: true,
	header: func(params tools.LSPRestartParams) []string {
		var toolParams []string
		if params.Name != "" {
			toolParams = append(toolParams, params.Name)
		}
		return toolParams
	},
	body: plainResultBody[tools.LSPRestartParams],
}
