package chat

import (
	"encoding/json"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
)

// DefinitionToolMessageItem is a message item that represents a definition tool call.
type DefinitionToolMessageItem struct {
	*baseToolMessageItem
}

var _ ToolMessageItem = (*DefinitionToolMessageItem)(nil)

// NewDefinitionToolMessageItem creates a new [DefinitionToolMessageItem].
func NewDefinitionToolMessageItem(
	sty *styles.Styles,
	toolCall message.ToolCall,
	result *message.ToolResult,
	canceled bool,
) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &DefinitionToolRenderContext{}, canceled)
}

// DefinitionToolRenderContext renders definition tool messages.
type DefinitionToolRenderContext struct{}

// RenderTool implements the [ToolRenderer] interface.
func (r *DefinitionToolRenderContext) RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string {
	return definitionTool.render(sty, width, opts)
}

// definitionTool draws Find Definition tool calls with the standard layout.
var definitionTool = standardTool[tools.DefinitionParams]{
	name:    "Find Definition",
	lenient: true,
	header: func(params tools.DefinitionParams) []string {
		return []string{params.Symbol}
	},
	body: func(sty *styles.Styles, opts *ToolRenderOpts, params tools.DefinitionParams, cappedWidth int) string {
		// Try to render code with syntax highlighting using metadata.
		var meta tools.DefinitionResponseMetadata
		if err := json.Unmarshal([]byte(opts.Result.Metadata), &meta); err == nil && meta.Content != "" {
			return toolOutputCodeContent(sty, meta.FilePath, meta.Content, 0, cappedWidth, opts.ExpandedContent)
		}

		// Fallback to plain text.
		return plainResultBody(sty, opts, params, cappedWidth)
	},
}
