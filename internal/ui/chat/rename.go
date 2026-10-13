package chat

import (
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/fsext"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
)

// RenameToolMessageItem is a message item that represents a rename tool call.
type RenameToolMessageItem struct {
	*baseToolMessageItem
}

var _ ToolMessageItem = (*RenameToolMessageItem)(nil)

// NewRenameToolMessageItem creates a new [RenameToolMessageItem].
func NewRenameToolMessageItem(
	sty *styles.Styles,
	toolCall message.ToolCall,
	result *message.ToolResult,
	canceled bool,
) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &RenameToolRenderContext{}, canceled)
}

// RenameToolRenderContext renders rename tool messages.
type RenameToolRenderContext struct{}

// RenderTool implements the [ToolRenderer] interface.
func (r *RenameToolRenderContext) RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string {
	return renameTool.render(sty, width, opts)
}

// renameTool draws Rename Symbol tool calls with the standard layout.
var renameTool = standardTool[tools.RenameParams]{
	name:    "Rename Symbol",
	lenient: true,
	header: func(params tools.RenameParams) []string {
		toolParams := []string{params.Symbol + " → " + params.NewName}
		if params.Path != "" {
			toolParams = append(toolParams, "path", fsext.PrettyPath(params.Path))
		}
		return toolParams
	},
	body: plainResultBody[tools.RenameParams],
}
