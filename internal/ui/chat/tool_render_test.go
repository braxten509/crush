package chat

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/exp/golden"
)

// toolRenderCases are calls to the tools drawn with the standard layout,
// with an input and result that reach their body.
var toolRenderCases = []struct {
	name, input, content, metadata string
}{
	{"glob", `{"pattern":"*.go","path":"internal"}`, "a.go\nb.go", ""},
	{"grep", `{"pattern":"func","path":"internal","include":"*.go","literal_text":true}`, "a.go:1: func main()", ""},
	{"ls", `{"path":""}`, "- a.go\n- b.go", ""},
	{"sourcegraph", `{"query":"repo:x foo","count":3,"context_window":2}`, "result", ""},
	{"fetch", `{"url":"https://example.com","format":"html","timeout":5}`, "<p>hi</p>", ""},
	{"web_fetch", `{"url":"https://example.com"}`, "# Title\n\nBody", ""},
	{"web_search", `{"query":"crush"}`, "- [Crush](https://charm.land)", ""},
	{"download", `{"url":"https://example.com/f","file_path":"f.bin","timeout":30}`, "saved", ""},
	{"lsp_references", `{"symbol":"Foo","path":"a.go"}`, "a.go:1:1", ""},
	{"lsp_definition", `{"symbol":"Foo"}`, "a.go:1", `{"file_path":"a.go","content":"func Foo() {}"}`},
	{"lsp_definition", `{"symbol":"Foo"}`, "a.go:1", ""},
	{"lsp_rename", `{"symbol":"Foo","new_name":"Bar","path":"a.go"}`, "renamed 2", ""},
	{"lsp_call_hierarchy", `{"symbol":"Foo","direction":"outgoing"}`, "Foo -> Bar", ""},
	{"lsp_symbols", `{"file_path":"a.go"}`, "Foo\n  Bar", ""},
	{"lsp_diagnostics", `{"file_path":"a.go"}`, "no issues", ""},
	{"lsp_diagnostics", `{}`, "no issues", ""},
	{"lsp_restart", `{"name":"gopls"}`, "restarted", ""},
	{"lsp_restart", `{}`, "restarted", ""},
	// Inputs that do not decode: strict tools report it, LSP tools draw
	// with empty parameters.
	{"glob", `not json`, "a.go", ""},
	{"lsp_symbols", `not json`, "Foo", ""},
}

// TestStandardToolRendering pins the output of the tools that share the
// standard layout, in each state they can be drawn in.
func TestStandardToolRendering(t *testing.T) {
	t.Parallel()

	sty := styles.CharmtonePantera()
	var b strings.Builder
	for _, tc := range toolRenderCases {
		call := message.ToolCall{ID: "call-1", Name: tc.name, Input: tc.input, Finished: true}
		result := &message.ToolResult{ToolCallID: "call-1", Name: tc.name, Content: tc.content, Metadata: tc.metadata}
		states := []struct {
			label    string
			result   *message.ToolResult
			canceled bool
			status   ToolStatus
			compact  bool
			expanded bool
		}{
			{label: "running", status: ToolStatusRunning},
			{label: "canceled", canceled: true, status: ToolStatusCanceled},
			{label: "success", result: result, status: ToolStatusSuccess},
			{label: "expanded", result: result, status: ToolStatusSuccess, expanded: true},
			{label: "compact", result: result, status: ToolStatusSuccess, compact: true},
			{label: "empty", result: &message.ToolResult{ToolCallID: "call-1"}, status: ToolStatusSuccess},
			{label: "error", result: &message.ToolResult{ToolCallID: "call-1", Content: "boom", IsError: true}, status: ToolStatusError},
		}
		for _, st := range states {
			item := NewToolMessageItem(&sty, "msg-1", call, st.result, st.canceled, "/work")
			item.SetStatus(st.status)
			if st.compact {
				item.(Compactable).SetCompact(true)
			}
			if st.expanded {
				item.(Expandable).ToggleExpanded()
			}
			fmt.Fprintf(&b, "=== %s %s %s\n%s\n\n", tc.name, tc.input, st.label, item.Render(80))
		}
	}
	golden.RequireEqual(t, []byte(b.String()))
}
