package chat

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/exp/golden"
)

// TestDockerMCPToolRendering pins how Docker MCP tool calls are drawn: the
// action names, which parameter leads, the extra parameters, and each kind
// of body.
func TestDockerMCPToolRendering(t *testing.T) {
	t.Parallel()

	servers := func(n int) string {
		var entries []string
		for i := range n {
			entries = append(entries, fmt.Sprintf(`{"name":"server-%d","description":"Server number %d"}`, i, i))
		}
		return `{"servers":[` + strings.Join(entries, ",") + `]}`
	}
	png := base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\nnot really a picture"))
	cases := []struct {
		tool, input, content string
		result               *message.ToolResult
	}{
		{tool: "mcp-find", input: `{"query":"github","limit":5,"verbose":true}`, content: servers(3)},
		{tool: "mcp-find", input: `{"query":"github"}`, content: servers(12)},
		{tool: "mcp-find", input: `{"query":"none"}`, content: servers(0)},
		{tool: "mcp-find", input: `{"query":"bad"}`, content: "not json output"},
		{tool: "mcp-find", input: `{"query":"empty"}`, content: ""},
		{tool: "mcp-find", input: `{"query":42}`, content: servers(1)},
		{tool: "mcp-add", input: `{"name":"github","activate":true,"config":{"a":1}}`, content: "added"},
		{tool: "mcp-add", input: `{"name":7}`, content: "added"},
		{tool: "mcp-remove", input: `{"name":"github","force":false}`, content: "removed"},
		{tool: "mcp-exec", input: `{"name":"search","arguments":{"q":"x"}}`, content: "line one\nline two"},
		{tool: "mcp-config-set", input: `{"server":"github","key":"token"}`, content: "set"},
		{tool: "code-mode", input: `{"script":"print(1)"}`, content: "1"},
		{tool: "list_tools", input: `{}`, content: "a\nb"},
		{tool: "mcp-exec", input: `not json`, content: "ran"},
		{tool: "mcp-exec", input: `{"name":"shot"}`, result: &message.ToolResult{Data: png, MIMEType: "image/png"}},
		{tool: "mcp-exec", input: `{"name":"shot"}`, result: &message.ToolResult{Content: "with image", Data: png, MIMEType: "image/png"}},
		{tool: "mcp-exec", input: `{"name":"blob"}`, result: &message.ToolResult{Data: png, MIMEType: "application/octet-stream"}},
	}

	sty := styles.CharmtonePantera()
	r := &DockerMCPToolRenderContext{}
	var b strings.Builder
	for _, tc := range cases {
		name := "mcp_docker_" + tc.tool
		call := message.ToolCall{ID: "call-1", Name: name, Input: tc.input, Finished: true}
		result := tc.result
		if result == nil {
			result = &message.ToolResult{Content: tc.content}
		}
		result.ToolCallID, result.Name = "call-1", name
		states := []struct {
			label string
			opts  ToolRenderOpts
		}{
			{"success", ToolRenderOpts{Result: result, Status: ToolStatusSuccess}},
			{"expanded", ToolRenderOpts{Result: result, Status: ToolStatusSuccess, ExpandedContent: true}},
			{"compact", ToolRenderOpts{Result: result, Status: ToolStatusSuccess, Compact: true}},
			{"no result", ToolRenderOpts{Status: ToolStatusSuccess}},
			{"running", ToolRenderOpts{Status: ToolStatusRunning}},
			{"canceled", ToolRenderOpts{Status: ToolStatusCanceled}},
			{"error", ToolRenderOpts{Result: &message.ToolResult{ToolCallID: "call-1", Content: "boom", IsError: true}, Status: ToolStatusError}},
		}
		for _, st := range states {
			opts := st.opts
			opts.ToolCall = call
			for _, width := range []int{40, 100} {
				fmt.Fprintf(&b, "=== %s %s %s @%d\n%s\n\n", tc.tool, tc.input, st.label, width, r.RenderTool(&sty, width, &opts))
			}
		}
	}
	golden.RequireEqual(t, []byte(b.String()))
	if !IsDockerMCPTool("mcp_docker_mcp-find") || IsDockerMCPTool("mcp_github_search") {
		t.Fatal("IsDockerMCPTool misclassified a tool name")
	}
}
