package chat

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"
)

// TestToolGroupHeaderStates pins the group header, including its styling
// and the clickable action-count columns, across the states it can show.
// While working, the animated activity after " | " is left out, since it
// changes from frame to frame.
func TestToolGroupHeaderStates(t *testing.T) {
	t.Parallel()

	sty := styles.CharmtonePantera()
	tool := func(id, name, input string, result *message.ToolResult, status ToolStatus) MessageItem {
		tc := message.ToolCall{ID: id, Name: name, Input: input, Finished: true}
		if result != nil {
			result.ToolCallID, result.Name = id, name
		}
		item := NewToolMessageItem(&sty, "m-"+id, tc, result, false, "")
		item.SetStatus(status)
		return item
	}
	ok := func(content string) *message.ToolResult { return &message.ToolResult{Content: content} }
	bash := func(id string) MessageItem {
		return tool(id, "bash", `{"command":"ls"}`, ok("ok"), ToolStatusSuccess)
	}
	failed := func(id string) MessageItem {
		return tool(id, "bash", `{"command":"false"}`, &message.ToolResult{Content: "exit 1", IsError: true}, ToolStatusError)
	}
	running := func(id, name, input string) MessageItem {
		return tool(id, name, input, nil, ToolStatusRunning)
	}
	edit := func(id, path string) MessageItem {
		return tool(id, "edit", `{"file_path":"`+path+`","old_string":"x","new_string":"y"}`,
			&message.ToolResult{Content: "ok", Metadata: `{"additions":1,"removals":1,"old_content":"a\nx\n","new_content":"a\ny\nz\n"}`},
			ToolStatusSuccess)
	}
	legacyEdit := func(id, path string) MessageItem {
		return tool(id, "write", `{"file_path":"`+path+`"}`, ok("ok"), ToolStatusSuccess)
	}
	reviewed := func(id string, changes ...filechange.Change) MessageItem {
		return tool(id, "bash", `{"command":"script"}`,
			&message.ToolResult{Content: "ok", Review: &filechange.Review{Root: "/w", Changes: changes}}, ToolStatusSuccess)
	}
	stored := func(id string, summary filechange.ReviewSummary) MessageItem {
		return tool(id, "bash", `{"command":"script"}`,
			&message.ToolResult{Content: "ok", Review: &filechange.Review{ID: id, Summary: &summary}}, ToolStatusSuccess)
	}
	text := func(s string) *filechange.State { return &filechange.State{Content: s, Mode: 0o644} }
	thinking := func(id string) MessageItem {
		msg := &message.Message{ID: id, Role: message.Assistant}
		msg.AppendReasoningContent("Considering.")
		return NewAssistantMessageItem(&sty, msg)
	}

	cases := []struct {
		name     string
		children []MessageItem
		live     bool
		busy     bool
		expanded bool
	}{
		{name: "one action", children: []MessageItem{bash("a")}},
		{name: "expanded", children: []MessageItem{bash("a"), bash("b")}, expanded: true},
		{name: "last failed", children: []MessageItem{bash("a"), failed("b")}},
		{name: "earlier failed", children: []MessageItem{failed("a"), failed("b"), bash("c")}},
		{name: "backgrounded", children: []MessageItem{tool("a", "bash", `{"command":"sleep 9"}`, &message.ToolResult{Content: "moved", Metadata: `{"background":true}`}, ToolStatusSuccess)}},
		{name: "backgrounded earlier", children: []MessageItem{tool("a", "bash", `{"command":"sleep 9"}`, &message.ToolResult{Content: "moved", Metadata: `{"background":true}`}, ToolStatusSuccess), bash("b")}},
		{name: "edits", children: []MessageItem{edit("a", "/p/a.go"), edit("b", "/p/b.go")}},
		{name: "one edit", children: []MessageItem{edit("a", "/p/a-very-long-file-name-for-the-summary.go")}},
		{name: "legacy edits", children: []MessageItem{legacyEdit("a", "/p/a.go"), legacyEdit("b", "/p/b.go"), bash("c")}},
		{name: "review edit", children: []MessageItem{reviewed("a", filechange.Change{Path: "/w/a.txt", Before: text("a\n"), After: text("b\n")})}},
		{name: "review removed", children: []MessageItem{reviewed("a", filechange.Change{Path: "/w/a.txt", Before: text("a\n")})}},
		{name: "review mixed", children: []MessageItem{reviewed("a",
			filechange.Change{Path: "/w/a.txt", Before: text("a\n")},
			filechange.Change{Path: "/w/b.txt", Before: text("a\n"), After: text("b\nc\n")},
		)}},
		{name: "stored summary counted", children: []MessageItem{stored("a", filechange.ReviewSummary{Files: 2, Paths: []string{"/w/a", "/w/b"}, Adds: 3, Dels: 1, Counted: true})}},
		{name: "stored summary uncounted", children: []MessageItem{stored("a", filechange.ReviewSummary{Files: 1, Paths: []string{"/w/a"}})}},
		{name: "stored deletions", children: []MessageItem{stored("a", filechange.ReviewSummary{Deletions: true})}},
		{name: "idle live", children: []MessageItem{bash("a")}, live: true},
		{name: "busy between steps", children: []MessageItem{bash("a")}, live: true, busy: true},
		{name: "thinking", children: []MessageItem{bash("a"), thinking("t")}, live: true, busy: true},
		{name: "running command", children: []MessageItem{bash("a"), running("b", "bash", `{"command":"make"}`)}, live: true, busy: true},
		{name: "running edit after edits", children: []MessageItem{edit("a", "/p/a.go"), running("b", "edit", `{"file_path":"/p/b.go"}`)}, live: true, busy: true},
		{name: "running after legacy edit", children: []MessageItem{legacyEdit("a", "/p/a.go"), running("b", "view", `{"file_path":"/p/b.go"}`)}, live: true, busy: true},
		{name: "running mcp", children: []MessageItem{running("a", "mcp_github_search", `{}`)}, live: true, busy: true},
		{name: "awaiting permission", children: []MessageItem{tool("a", "bash", `{"command":"rm"}`, nil, ToolStatusAwaitingPermission)}, live: true, busy: true},
		{name: "failed while busy", children: []MessageItem{failed("a"), bash("b")}, live: true, busy: true},
	}

	var b strings.Builder
	for _, tc := range cases {
		g := NewToolGroupItem(&sty)
		g.SetChildren(tc.children)
		g.SetLive(tc.live)
		g.SetBusy(tc.busy)
		if tc.expanded {
			g.ToggleExpanded()
		}
		for _, width := range []int{12, 40, 120} {
			header := g.header(width)
			if g.status() != "" {
				plain := ansi.Strip(header)
				if i := strings.LastIndex(plain, " | "); i >= 0 {
					header = plain[:i] + " | <activity>"
				}
			}
			fmt.Fprintf(&b, "=== %s @%d cols=%v status=%q\n%s\n\n", tc.name, width, g.changesCols, g.status(), header)
		}
	}
	golden.RequireEqual(t, []byte(b.String()))
}
