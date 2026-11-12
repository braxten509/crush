package chat

import (
	"testing"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/stretchr/testify/require"
)

// toolCopyCase is one tool call the registry test copies. want, when set,
// is the main input value the copied text must carry.
type toolCopyCase struct {
	name  string
	input string
	want  string
}

// toolCopyCases covers every built-in tool the chat knows, the MCP
// prefixes, and tools that fall back to the generic item. Inputs with
// more than one key go through a specific formatter only: the generic
// formatter ranges over a map, so its order is not stable.
var toolCopyCases = []toolCopyCase{
	{"bash", `{"command":"ls -la\n\techo hi"}`, "ls -la"},
	{"job_output", `{"shell_id":"1"}`, ""},
	{"job_kill", `{"shell_id":"1"}`, ""},
	{"view", `{"file_path":"/tmp/x.go","limit":10,"offset":5}`, "/tmp/x.go"},
	{"write", `{"file_path":"a.go","content":"x"}`, ""},
	{"edit", `{"file_path":"a.go","old_string":"a","new_string":"b"}`, ""},
	{"multiedit", `{"file_path":"a.go","edits":[{"old_string":"a","new_string":"b"}]}`, ""},
	{"glob", `{"pattern":"*.go","path":"d"}`, ""},
	{"grep", `{"pattern":"needle","path":"d","include":"*.go","literal_text":true}`, "needle"},
	{"ls", `{"path":""}`, ""},
	{"download", `{"url":"https://x","file_path":"f","timeout":30}`, ""},
	{"fetch", `{"url":"https://x","format":"text","timeout":5}`, "https://x"},
	{"sourcegraph", `{"query":"q","count":3,"context_window":2}`, ""},
	{"lsp_diagnostics", `{"file_path":"a.go"}`, ""},
	{"agent", `{"prompt":"do it"}`, ""},
	{"agentic_fetch", `{"url":"https://x","prompt":"p"}`, ""},
	{"web_fetch", `{"url":"https://x"}`, ""},
	{"web_search", `{"query":"q"}`, ""},
	{"todos", `{"todos":[]}`, ""},
	{"question", `{"questions":[]}`, ""},
	{"lsp_references", `{"symbol":"Foo"}`, ""},
	{"lsp_definition", `{"symbol":"Foo"}`, ""},
	{"lsp_rename", `{"symbol":"Foo"}`, ""},
	{"lsp_replace_symbol", `{"symbol":"Foo"}`, ""},
	{"lsp_call_hierarchy", `{"symbol":"Foo"}`, ""},
	{"lsp_symbols", `{"query":"Foo"}`, ""},
	{"lsp_restart", `{"name":"gopls"}`, ""},
	{"mcp_docker_search", `{"query":"q"}`, ""},
	{"mcp_github_issues", `{"repo":"r"}`, ""},
	{"crush_info", `{"topic":"t"}`, ""},
	{"unknown_tool", `{"some_key":"v"}`, ""},
	// Unparsable or mistyped input falls back to the generic formatter.
	{"bash", `{"command":5}`, ""},
	{"view", `not json`, ""},
}

// TestToolMessageItemRegistry checks that every tool gets an item that can
// be copied, in each state, and that the copy carries the main input.
func TestToolMessageItemRegistry(t *testing.T) {
	t.Parallel()

	sty := styles.CharmtonePantera()
	for _, tc := range toolCopyCases {
		call := message.ToolCall{ID: "call-1", Name: tc.name, Input: tc.input, Finished: true}
		states := []struct {
			label    string
			result   *message.ToolResult
			canceled bool
		}{
			{"pending", nil, false},
			{"canceled", nil, true},
			{"result", &message.ToolResult{ToolCallID: "call-1", Name: tc.name, Content: "line 1\nline 2"}, false},
			{"error", &message.ToolResult{ToolCallID: "call-1", Name: tc.name, Content: "boom", IsError: true}, false},
			{"image", &message.ToolResult{ToolCallID: "call-1", Name: tc.name, Data: "AAAA", MIMEType: "image/png"}, false},
		}
		for _, st := range states {
			item := NewToolMessageItem(&sty, "msg-1", call, st.result, st.canceled, "/work")
			copier, ok := item.(interface{ formatToolForCopy() string })
			require.True(t, ok, "tool item %T cannot be copied", item)
			copied := copier.formatToolForCopy()
			require.NotEmpty(t, copied, "%s %s %s", tc.name, tc.input, st.label)
			if tc.want != "" {
				require.Contains(t, copied, tc.want, "%s %s", tc.name, st.label)
			}
		}
	}
}
