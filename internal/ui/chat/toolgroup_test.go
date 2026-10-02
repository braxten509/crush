package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/list"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// A live group keeps a status between steps while the agent is busy.
func TestToolGroupStatusBetweenSteps(t *testing.T) {
	t.Parallel()

	sty := styles.CharmtonePantera()
	tc := message.ToolCall{ID: "b1", Name: "bash", Input: `{"command":"ls"}`, Finished: true}
	done := NewToolMessageItem(&sty, "m1", tc, &message.ToolResult{ToolCallID: "b1", Content: "ok"}, false, "")

	g := NewToolGroupItem(&sty)
	g.SetChildren([]MessageItem{done})
	g.SetLive(true)
	require.Empty(t, g.status(), "idle agent: no status")

	g.SetBusy(true)
	require.Equal(t, "Thinking", g.status())
	require.True(t, g.Spinning())

	g.SetLive(false)
	require.Empty(t, g.status(), "only the live group shows a status")
}

func TestToolGroupFilesystemReviewAndUpdates(t *testing.T) {
	t.Parallel()
	sty := styles.CharmtonePantera()
	tc := message.ToolCall{ID: "shell", Name: "bash", Input: `{"command":"run-script"}`, Finished: true}
	result := message.ToolResult{ToolCallID: tc.ID, Name: tc.Name, IsError: true, Content: "exit 7"}
	item := NewToolMessageItem(&sty, "message", tc, &result, false, "")
	g := NewToolGroupItem(&sty)
	g.SetChildren([]MessageItem{item})
	require.Empty(t, g.Changes())

	// A saved result can receive a final snapshot after the process exits.
	// The same tool ID must invalidate the cached diff, even on an error.
	result.Review = &filechange.Review{Root: "/workspace", Changes: []filechange.Change{{
		Path:   "/workspace/shell.txt",
		Before: &filechange.State{Content: "before\n", Mode: 0o644},
		After:  &filechange.State{Content: "after\n", Mode: 0o644},
	}}}
	item.SetResult(&result)
	require.Len(t, g.Changes(), 1)
	header := ansi.Strip(g.header(120))
	require.Contains(t, header, "edited shell.txt +1 −1")
	require.Greater(t, g.changesCols[1], g.changesCols[0])

	result.Review = &filechange.Review{Root: "/workspace", Changes: []filechange.Change{{
		Path: "/workspace/empty", After: &filechange.State{Mode: 0o644},
	}}}
	item.SetResult(&result)
	require.Equal(t, "/workspace/empty", g.Changes()[0].Path)
	require.Contains(t, ansi.Strip(g.header(120)), "edited empty +0 −0")
	require.Greater(t, g.changesCols[1], g.changesCols[0], "non-text changes must be clickable")
}

func TestToolGroupSnapshotOverridesLegacyMetadata(t *testing.T) {
	t.Parallel()
	sty := styles.CharmtonePantera()
	tc := message.ToolCall{ID: "edit", Name: "edit", Input: `{"file_path":"/workspace/file"}`, Finished: true}
	result := &message.ToolResult{
		ToolCallID: tc.ID, Name: tc.Name, Metadata: `{"old_content":"wrong","new_content":"diff"}`,
		Review: &filechange.Review{Root: "/workspace"},
	}
	g := NewToolGroupItem(&sty)
	g.SetChildren([]MessageItem{NewToolMessageItem(&sty, "message", tc, result, false, "")})
	require.Empty(t, g.Changes(), "the actual filesystem stayed unchanged")
}

// A command moved to the background isn't shown as finished.
func TestToolGroupShowsBackgroundedCommand(t *testing.T) {
	t.Parallel()

	sty := styles.CharmtonePantera()
	bash := func(id, metadata string) MessageItem {
		tc := message.ToolCall{ID: id, Name: "bash", Input: `{"command":"sleep 60"}`, Finished: true}
		return NewToolMessageItem(&sty, "m1", tc, &message.ToolResult{ToolCallID: id, Name: "bash", Content: "moved", Metadata: metadata}, false, "")
	}

	g := NewToolGroupItem(&sty)
	g.SetChildren([]MessageItem{bash("b1", `{"background":true}`)})
	header := ansi.Strip(g.header(80))
	require.Contains(t, header, "⚙ 1 action · 1 backgrounded")
	require.NotContains(t, header, "✓")
	require.Contains(t, ansi.Strip(g.children[0].(ToolMessageItem).RawRender(80)), "background=true")

	// A Crush background command that already ended counts as finished.
	g.SetChildren([]MessageItem{bash("b2", `{"background":true,"end_time":5}`)})
	header = ansi.Strip(g.header(80))
	require.NotContains(t, header, "backgrounded")
	require.NotContains(t, header, "⚙")
}

// The action-count label opens changes; the disclosure and file summary
// retain the group's expansion behavior.
func TestToolGroupChanges(t *testing.T) {
	t.Parallel()

	sty := styles.CharmtonePantera()
	edit := func(id, input, metadata string) MessageItem {
		tc := message.ToolCall{ID: id, Name: "edit", Input: input, Finished: true}
		return NewToolMessageItem(&sty, "m1", tc, &message.ToolResult{ToolCallID: id, Name: "edit", Content: "ok", Metadata: metadata}, false, "")
	}
	bash := NewToolMessageItem(&sty, "m1", message.ToolCall{ID: "b1", Name: "bash", Input: `{"command":"ls"}`, Finished: true},
		&message.ToolResult{ToolCallID: "b1", Content: "ok"}, false, "")

	g := NewToolGroupItem(&sty)
	g.SetChildren([]MessageItem{bash})
	require.Empty(t, g.Changes())
	require.NotContains(t, ansi.Strip(g.header(120)), "+")

	g.SetChildren([]MessageItem{
		bash,
		edit("e1", `{"file_path":"/p/a.go","old_string":"x","new_string":"y"}`,
			`{"additions":1,"removals":1,"old_content":"a\nx\n","new_content":"a\ny\nz\n"}`),
		// Older CLI edits kept only the replaced text.
		edit("e2", `{"file_path":"/p/b.go","old_string":"q","new_string":"r"}`, `{"old_content":"q","new_content":"r"}`),
	})
	files := g.Changes()
	require.Len(t, files, 2)
	require.Equal(t, 3, files[0].Lines[len(files[0].Lines)-1].New, "whole-file edits have line numbers")
	require.Zero(t, files[1].Lines[0].Old, "replaced text has no place in the file")

	header := ansi.Strip(g.header(120))
	require.Contains(t, header, "3 actions · edited a.go, b.go +3 −2")

	// Click the original action text, immediately after the success icon.
	start := MessageLeftPaddingTotal + ansi.StringWidth(header[:strings.Index(header, "3 actions")])
	require.Equal(t, start, g.changesCols[0])
	require.Equal(t, start+len("3 actions"), g.changesCols[1])
	require.False(t, g.HandleMouseClick(ansi.MouseLeft, start+1, 0), "no expand")
	require.True(t, g.TakeChangesRequest())
	require.False(t, g.TakeChangesRequest(), "taken once")

	require.True(t, g.HandleMouseClick(ansi.MouseLeft, 3, 0), "elsewhere expands")
	require.False(t, g.TakeChangesRequest())
	summary := MessageLeftPaddingTotal + ansi.StringWidth(header[:strings.Index(header, "edited")])
	require.True(t, g.HandleMouseClick(ansi.MouseLeft, summary, 0), "file summary is not the review link")
	require.False(t, g.TakeChangesRequest())

	// Resizing cannot leave an invisible review target beyond the rendered text.
	narrow := ansi.Strip(g.header(10))
	require.LessOrEqual(t, g.changesCols[1], MessageLeftPaddingTotal+ansi.StringWidth(narrow))
	require.True(t, g.HandleMouseClick(ansi.MouseLeft, summary, 0))
	require.False(t, g.TakeChangesRequest())
	require.Equal(t, "3 actions", g.ChangesTitle())
}

func TestToolGroupHidesTemporaryAndCacheReviews(t *testing.T) {
	sty := styles.CharmtonePantera()
	tc := message.ToolCall{ID: "build", Name: "bash", Input: `{"command":"go test ./..."}`, Finished: true}
	review := &filechange.Review{Root: "/workspace"}
	for _, path := range []string{"/tmp/go-build/item", "/var/tmp/scratch", "/workspace/.cache/build", ".cache/local", "/workspace/main.go"} {
		review.Changes = append(review.Changes, filechange.Change{Path: path, After: &filechange.State{Content: "new\n"}})
	}
	result := &message.ToolResult{ToolCallID: tc.ID, Name: tc.Name, Review: review}
	item := NewToolMessageItem(&sty, "message", tc, result, false, "")
	group := NewToolGroupItem(&sty)
	group.SetChildren([]MessageItem{item})
	require.Len(t, group.Changes(), 1)
	require.Equal(t, "/workspace/main.go", group.Changes()[0].Path)
	require.Contains(t, ansi.Strip(group.header(120)), "edited main.go +1 −0")
	review.Changes = review.Changes[:len(review.Changes)-1]
	item.SetResult(result)
	require.Empty(t, group.Changes())
	require.NotContains(t, ansi.Strip(group.header(120)), "edited")
	require.Equal(t, [2]int{}, group.changesCols)
}

func TestIgnoredReviewPaths(t *testing.T) {
	t.Setenv("TMPDIR", "/scratch/runtime")
	t.Setenv("XDG_CACHE_HOME", "/custom/cache")
	t.Setenv("XDG_CONFIG_HOME", "/custom/config")
	for _, path := range []string{"/tmp/a", "/var/tmp/a", "/dev/shm/a", "/scratch/runtime/a", "/custom/cache/a", ".cache/a", "/work/.cache/a", "/custom/config/go/telemetry/local/count", "/custom/config/app/settings.json"} {
		require.True(t, ignoredReviewPath(path, ""), path)
	}
	for _, path := range []string{"/tmp-source/main.go", "/workspace/tmp/main.go", "/workspace/cache.go", "/custom/cache-source/main.go", "/custom/config-source/main.go", "/home/user/Desktop/hello.txt"} {
		require.False(t, ignoredReviewPath(path, ""), path)
	}
	require.True(t, ignoredReviewPath("relative.txt", "/tmp/project"))
	require.True(t, ignoredReviewPath("../.cache/item", "/workspace/project"))
}

func TestToolGroupHidesLegacyTemporaryEdits(t *testing.T) {
	sty := styles.CharmtonePantera()
	tc := message.ToolCall{ID: "edit", Name: "edit", Input: `{"file_path":"/tmp/scratch"}`, Finished: true}
	result := &message.ToolResult{ToolCallID: tc.ID, Name: tc.Name, Metadata: `{"old_content":"old","new_content":"new"}`}
	group := NewToolGroupItem(&sty)
	group.SetChildren([]MessageItem{NewToolMessageItem(&sty, "message", tc, result, false, "")})
	require.Empty(t, group.Changes())
	require.NotContains(t, ansi.Strip(group.header(120)), "edited")
	require.Equal(t, [2]int{}, group.changesCols)
}

// Dropping a step must move the group's version, or the list keeps
// serving the render cached for the old steps.
func TestToolGroupVersionMovesWhenStepRemoved(t *testing.T) {
	t.Parallel()

	sty := styles.CharmtonePantera()
	mk := func(id string) MessageItem {
		tc := message.ToolCall{ID: id, Name: "bash", Input: `{"command":"ls"}`, Finished: true}
		return NewToolMessageItem(&sty, "m1", tc, &message.ToolResult{ToolCallID: id, Content: "ok"}, false, "")
	}
	t1, t2 := mk("a"), mk("b")
	g := NewToolGroupItem(&sty)
	g.SetChildren([]MessageItem{t1, t2})
	l := list.NewList()
	l.SetSize(100, 40)
	l.SetItems(g)
	require.Contains(t, ansi.Strip(l.Render()), "2 actions")

	before := g.Version()
	g.SetChildren([]MessageItem{t1})
	require.Greater(t, g.Version(), before)
	require.NotContains(t, ansi.Strip(l.Render()), "2 actions")
}
