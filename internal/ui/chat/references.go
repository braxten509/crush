package chat

import (
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/fsext"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
)

// ReferencesToolMessageItem is a message item that represents a references tool call.
type ReferencesToolMessageItem struct {
	*baseToolMessageItem
}

var _ ToolMessageItem = (*ReferencesToolMessageItem)(nil)

// NewReferencesToolMessageItem creates a new [ReferencesToolMessageItem].
func NewReferencesToolMessageItem(
	sty *styles.Styles,
	toolCall message.ToolCall,
	result *message.ToolResult,
	canceled bool,
) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &ReferencesToolRenderContext{}, canceled)
}

// ReferencesToolRenderContext renders references tool messages.
type ReferencesToolRenderContext struct{}

// RenderTool implements the [ToolRenderer] interface.
func (r *ReferencesToolRenderContext) RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string {
	return referencesTool.render(sty, width, opts)
}

// referencesTool draws Find References tool calls with the standard layout.
var referencesTool = standardTool[tools.ReferencesParams]{
	name:    "Find References",
	lenient: true,
	header: func(params tools.ReferencesParams) []string {
		toolParams := []string{params.Symbol}
		if params.Path != "" {
			toolParams = append(toolParams, "path", fsext.PrettyPath(params.Path))
		}
		return toolParams
	},
	body: plainResultBody[tools.ReferencesParams],
}
