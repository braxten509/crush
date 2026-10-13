package chat

import (
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/fsext"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
)

// -----------------------------------------------------------------------------
// Diagnostics Tool
// -----------------------------------------------------------------------------

// DiagnosticsToolMessageItem is a message item that represents a diagnostics tool call.
type DiagnosticsToolMessageItem struct {
	*baseToolMessageItem
}

var _ ToolMessageItem = (*DiagnosticsToolMessageItem)(nil)

// NewDiagnosticsToolMessageItem creates a new [DiagnosticsToolMessageItem].
func NewDiagnosticsToolMessageItem(
	sty *styles.Styles,
	toolCall message.ToolCall,
	result *message.ToolResult,
	canceled bool,
) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &DiagnosticsToolRenderContext{}, canceled)
}

// DiagnosticsToolRenderContext renders diagnostics tool messages.
type DiagnosticsToolRenderContext struct{}

// RenderTool implements the [ToolRenderer] interface.
func (d *DiagnosticsToolRenderContext) RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string {
	return diagnosticsTool.render(sty, width, opts)
}

// diagnosticsTool draws Diagnostics tool calls with the standard layout.
var diagnosticsTool = standardTool[tools.DiagnosticsParams]{
	name:    "Diagnostics",
	lenient: true,
	header: func(params tools.DiagnosticsParams) []string {
		// Show "project" if no file path, otherwise show the file path.
		mainParam := "project"
		if params.FilePath != "" {
			mainParam = fsext.PrettyPath(params.FilePath)
		}
		return []string{mainParam}
	},
	body: plainResultBody[tools.DiagnosticsParams],
}
