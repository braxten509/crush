package chat

import (
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
)

// -----------------------------------------------------------------------------
// Fetch Tool
// -----------------------------------------------------------------------------

// FetchToolMessageItem is a message item that represents a fetch tool call.
type FetchToolMessageItem struct {
	*baseToolMessageItem
}

var _ ToolMessageItem = (*FetchToolMessageItem)(nil)

// NewFetchToolMessageItem creates a new [FetchToolMessageItem].
func NewFetchToolMessageItem(
	sty *styles.Styles,
	toolCall message.ToolCall,
	result *message.ToolResult,
	canceled bool,
) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &FetchToolRenderContext{}, canceled)
}

// FetchToolRenderContext renders fetch tool messages.
type FetchToolRenderContext struct{}

// RenderTool implements the [ToolRenderer] interface.
func (f *FetchToolRenderContext) RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string {
	return fetchTool.render(sty, width, opts)
}

// fetchTool draws Fetch tool calls with the standard layout.
var fetchTool = standardTool[tools.FetchParams]{
	name: "Fetch",
	header: func(params tools.FetchParams) []string {
		toolParams := []string{params.URL}
		if params.Format != "" {
			toolParams = append(toolParams, "format", params.Format)
		}
		if params.Timeout != 0 {
			toolParams = append(toolParams, "timeout", formatTimeout(params.Timeout))
		}
		return toolParams
	},
	body: func(sty *styles.Styles, opts *ToolRenderOpts, params tools.FetchParams, cappedWidth int) string {
		// Determine file extension for syntax highlighting based on format.
		file := getFileExtensionForFormat(params.Format)
		return toolOutputCodeContent(sty, file, opts.Result.Content, 0, cappedWidth, opts.ExpandedContent)
	},
}

// getFileExtensionForFormat returns a filename with appropriate extension for syntax highlighting.
func getFileExtensionForFormat(format string) string {
	switch format {
	case "text":
		return "fetch.txt"
	case "html":
		return "fetch.html"
	default:
		return "fetch.md"
	}
}

// -----------------------------------------------------------------------------
// WebFetch Tool
// -----------------------------------------------------------------------------

// WebFetchToolMessageItem is a message item that represents a web_fetch tool call.
type WebFetchToolMessageItem struct {
	*baseToolMessageItem
}

var _ ToolMessageItem = (*WebFetchToolMessageItem)(nil)

// NewWebFetchToolMessageItem creates a new [WebFetchToolMessageItem].
func NewWebFetchToolMessageItem(
	sty *styles.Styles,
	toolCall message.ToolCall,
	result *message.ToolResult,
	canceled bool,
) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &WebFetchToolRenderContext{}, canceled)
}

// WebFetchToolRenderContext renders web_fetch tool messages.
type WebFetchToolRenderContext struct{}

// RenderTool implements the [ToolRenderer] interface.
func (w *WebFetchToolRenderContext) RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string {
	return webFetchTool.render(sty, width, opts)
}

// webFetchTool draws Fetch tool calls with the standard layout.
var webFetchTool = standardTool[tools.WebFetchParams]{
	name: "Fetch",
	header: func(params tools.WebFetchParams) []string {
		toolParams := []string{params.URL}
		return toolParams
	},
	body: func(sty *styles.Styles, opts *ToolRenderOpts, params tools.WebFetchParams, cappedWidth int) string {
		return toolOutputMarkdownContent(sty, opts.Result.Content, cappedWidth, opts.ExpandedContent)
	},
}

// -----------------------------------------------------------------------------
// WebSearch Tool
// -----------------------------------------------------------------------------

// WebSearchToolMessageItem is a message item that represents a web_search tool call.
type WebSearchToolMessageItem struct {
	*baseToolMessageItem
}

var _ ToolMessageItem = (*WebSearchToolMessageItem)(nil)

// NewWebSearchToolMessageItem creates a new [WebSearchToolMessageItem].
func NewWebSearchToolMessageItem(
	sty *styles.Styles,
	toolCall message.ToolCall,
	result *message.ToolResult,
	canceled bool,
) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &WebSearchToolRenderContext{}, canceled)
}

// WebSearchToolRenderContext renders web_search tool messages.
type WebSearchToolRenderContext struct{}

// RenderTool implements the [ToolRenderer] interface.
func (w *WebSearchToolRenderContext) RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string {
	return webSearchTool.render(sty, width, opts)
}

// webSearchTool draws Search tool calls with the standard layout.
var webSearchTool = standardTool[tools.WebSearchParams]{
	name: "Search",
	header: func(params tools.WebSearchParams) []string {
		toolParams := []string{params.Query}
		return toolParams
	},
	body: func(sty *styles.Styles, opts *ToolRenderOpts, params tools.WebSearchParams, cappedWidth int) string {
		return toolOutputMarkdownContent(sty, opts.Result.Content, cappedWidth, opts.ExpandedContent)
	},
}
