package chat

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/stringext"
	"github.com/charmbracelet/crush/internal/ui/styles"
)

// DockerMCPToolMessageItem is a message item that represents a Docker MCP tool call.
type DockerMCPToolMessageItem struct {
	*baseToolMessageItem
}

var _ ToolMessageItem = (*DockerMCPToolMessageItem)(nil)

// NewDockerMCPToolMessageItem creates a new [DockerMCPToolMessageItem].
func NewDockerMCPToolMessageItem(
	sty *styles.Styles,
	toolCall message.ToolCall,
	result *message.ToolResult,
	canceled bool,
) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &DockerMCPToolRenderContext{}, canceled)
}

// DockerMCPToolRenderContext renders Docker MCP tool messages.
type DockerMCPToolRenderContext struct{}

// RenderTool implements the [ToolRenderer] interface.
func (d *DockerMCPToolRenderContext) RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string {
	cappedWidth := cappedMessageWidth(width)
	tool := strings.TrimPrefix(opts.ToolCall.Name, "mcp_"+config.DockerMCPName+"_")
	toolParams := dockerMCPHeaderParams(tool, opts.ToolCall.Input)

	if opts.IsPending() {
		return pendingTool(sty, d.formatToolName(sty, tool), opts.Anim, false)
	}

	header := d.makeHeader(sty, tool, cappedWidth, opts, toolParams...)
	if opts.Compact {
		return header
	}

	if earlyState, ok := toolEarlyStateContent(sty, opts, cappedWidth); ok {
		return joinToolParts(header, earlyState)
	}

	if tool == "mcp-find" {
		return joinToolParts(header, d.renderMCPServers(sty, opts, cappedWidth))
	}

	if body, ok := dockerMCPResultContent(sty, opts, cappedWidth); ok {
		return joinToolParts(header, body)
	}
	return header
}

// dockerMCPParam is the parameter a Docker MCP tool's header leads with,
// and whether its other parameters follow.
type dockerMCPParam struct {
	key    string
	extras bool
}

// dockerMCPParams maps the Docker MCP tools with a leading parameter to it.
// Other tools show their raw input.
var dockerMCPParams = map[string]dockerMCPParam{
	"mcp-find":       {key: "query", extras: true},
	"mcp-add":        {key: "name", extras: true},
	"mcp-remove":     {key: "name", extras: true},
	"mcp-exec":       {key: "name"},
	"mcp-config-set": {key: "server"},
}

// dockerMCPHeaderParams returns the header parameters of a Docker MCP tool
// call: the leading one, then the extra key/value pairs sorted by key.
func dockerMCPHeaderParams(tool, input string) []string {
	var params map[string]any
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		params = make(map[string]any)
	}

	mainParam := input
	spec, ok := dockerMCPParams[tool]
	if !ok {
		return []string{mainParam}
	}
	if value, ok := params[spec.key].(string); ok {
		mainParam = value
	}
	toolParams := []string{mainParam}
	if !spec.extras {
		return toolParams
	}

	keys := make([]string, 0, len(params))
	for k := range params {
		if k != spec.key {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		data, _ := json.Marshal(params[k])
		toolParams = append(toolParams, k, string(data))
	}
	return toolParams
}

// dockerMCPResultContent renders a finished call's text and image content.
// It reports false when the call has no result or nothing to show.
func dockerMCPResultContent(sty *styles.Styles, opts *ToolRenderOpts, cappedWidth int) (string, bool) {
	if !opts.HasResult() {
		return "", false
	}

	bodyWidth := cappedWidth - toolBodyLeftPaddingTotal
	var parts []string

	// Handle text content.
	if opts.Result.Content != "" {
		body := renderToolResultTextContent(sty, opts.Result.Content, toolResultContentWidths{Body: bodyWidth, Diff: cappedWidth}, opts.ExpandedContent)
		parts = append(parts, body)
	}

	// Handle image content.
	if opts.Result.Data != "" && strings.HasPrefix(opts.Result.MIMEType, "image/") {
		parts = append(parts, "", toolOutputImageContent(sty, opts.Result.Data, opts.Result.MIMEType))
	}

	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, "\n"), true
}

// FindMCPResponse represents the response from mcp-find.
type FindMCPResponse struct {
	Servers []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"servers"`
}

func (d *DockerMCPToolRenderContext) renderMCPServers(sty *styles.Styles, opts *ToolRenderOpts, width int) string {
	if !opts.HasResult() || opts.Result.Content == "" {
		return ""
	}

	var result FindMCPResponse
	if err := json.Unmarshal([]byte(opts.Result.Content), &result); err != nil {
		return toolOutputPlainContent(sty, opts.Result.Content, width-toolBodyLeftPaddingTotal, opts.ExpandedContent)
	}

	if len(result.Servers) == 0 {
		return sty.Tool.ResultEmpty.Render("No MCP servers found.")
	}

	bodyWidth := min(120, width) - toolBodyLeftPaddingTotal
	rows := [][]string{}
	moreServers := ""
	for i, server := range result.Servers {
		if i > 9 {
			moreServers = sty.Tool.ResultTruncation.Render(fmt.Sprintf("... and %d more", len(result.Servers)-10))
			break
		}
		rows = append(rows, []string{sty.Tool.ResultItemName.Render(server.Name), sty.Tool.ResultItemDesc.Render(server.Description)})
	}
	serverTable := table.New().
		Wrap(false).
		BorderTop(false).
		BorderBottom(false).
		BorderRight(false).
		BorderLeft(false).
		BorderColumn(false).
		BorderRow(false).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return lipgloss.NewStyle()
			}
			switch col {
			case 0:
				return lipgloss.NewStyle().PaddingRight(1)
			}
			return lipgloss.NewStyle()
		}).Rows(rows...).Width(bodyWidth)
	if moreServers != "" {
		return sty.Tool.Body.Render(serverTable.Render() + "\n" + moreServers)
	}
	return sty.Tool.Body.Render(serverTable.Render())
}

func (d *DockerMCPToolRenderContext) makeHeader(sty *styles.Styles, tool string, width int, opts *ToolRenderOpts, params ...string) string {
	if opts.Compact {
		return d.makeCompactHeader(sty, tool, width, params...)
	}

	icon := toolIcon(sty, opts.Status)
	if opts.IsPending() {
		icon = sty.Tool.IconPending.Render()
	}
	prefix := fmt.Sprintf("%s %s ", icon, d.formatToolName(sty, tool))
	return prefix + toolParamList(sty, params, width-lipgloss.Width(prefix), opts)
}

func (d *DockerMCPToolRenderContext) formatToolName(sty *styles.Styles, tool string) string {
	mainTool := "Docker MCP"
	action := tool
	actionStyle := sty.Tool.MCPToolName
	switch tool {
	case "mcp-exec":
		action = "Exec"
	case "mcp-config-set":
		action = "Config Set"
	case "mcp-find":
		action = "Find"
	case "mcp-add":
		action = "Add"
		actionStyle = sty.Tool.ActionCreate
	case "mcp-remove":
		action = "Remove"
		actionStyle = sty.Tool.ActionDestroy
	case "code-mode":
		action = "Code Mode"
	default:
		action = strings.ReplaceAll(tool, "-", " ")
		action = strings.ReplaceAll(action, "_", " ")
		action = stringext.Capitalize(action)
	}

	toolNameStyled := sty.Tool.MCPName.Render(mainTool)
	arrow := sty.Tool.MCPArrow.String()
	return fmt.Sprintf("%s %s %s", toolNameStyled, arrow, actionStyle.Render(action))
}

func (d *DockerMCPToolRenderContext) makeCompactHeader(sty *styles.Styles, tool string, width int, params ...string) string {
	action := tool
	switch tool {
	case "mcp-exec":
		action = "exec"
	case "mcp-config-set":
		action = "config-set"
	case "mcp-find":
		action = "find"
	case "mcp-add":
		action = "add"
	case "mcp-remove":
		action = "remove"
	case "code-mode":
		action = "code-mode"
	default:
		action = strings.ReplaceAll(tool, "-", " ")
		action = strings.ReplaceAll(action, "_", " ")
	}

	name := fmt.Sprintf("Docker MCP: %s", action)
	return toolHeader(sty, ToolStatusSuccess, name, width, &ToolRenderOpts{Compact: true}, params...)
}

// IsDockerMCPTool returns true if the tool name is a Docker MCP tool.
func IsDockerMCPTool(name string) bool {
	return strings.HasPrefix(name, "mcp_"+config.DockerMCPName+"_")
}
