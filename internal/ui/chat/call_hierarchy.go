package chat

import (
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
)

// CallHierarchyToolMessageItem is a message item that represents a call hierarchy tool call.
type CallHierarchyToolMessageItem struct {
	*baseToolMessageItem
}

var _ ToolMessageItem = (*CallHierarchyToolMessageItem)(nil)

// NewCallHierarchyToolMessageItem creates a new [CallHierarchyToolMessageItem].
func NewCallHierarchyToolMessageItem(
	sty *styles.Styles,
	toolCall message.ToolCall,
	result *message.ToolResult,
	canceled bool,
) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &CallHierarchyToolRenderContext{}, canceled)
}

// CallHierarchyToolRenderContext renders call hierarchy tool messages.
type CallHierarchyToolRenderContext struct{}

// RenderTool implements the [ToolRenderer] interface.
func (r *CallHierarchyToolRenderContext) RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string {
	return callHierarchyTool.render(sty, width, opts)
}

// callHierarchyTool draws Call Hierarchy tool calls with the standard layout.
var callHierarchyTool = standardTool[tools.CallHierarchyParams]{
	name:    "Call Hierarchy",
	lenient: true,
	header: func(params tools.CallHierarchyParams) []string {
		direction := "incoming"
		if params.Direction == "outgoing" {
			direction = "outgoing"
		}
		return []string{params.Symbol, direction}
	},
	body: plainResultBody[tools.CallHierarchyParams],
}
