package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestSavedCloneReviewShowsCopiedCheckout(t *testing.T) {
	t.Parallel()
	sty := styles.CharmtonePantera()
	call := message.ToolCall{ID: "clone", Name: "bash", Finished: true,
		Input: `{"command":"git clone source destination"}`}
	result := &message.ToolResult{ToolCallID: call.ID, Name: call.Name,
		Review: &filechange.Review{Root: "/workspace", Changes: []filechange.Change{
			{Path: "/workspace/destination/main.go", After: &filechange.State{Content: strings.Repeat("line\n", 100_000)}},
		}}}
	g := NewToolGroupItem(&sty)
	g.SetChildren([]MessageItem{NewToolMessageItem(&sty, "saved", call, result, false, "")})
	header := ansi.Strip(g.header(160))
	require.Contains(t, header, "copied checkout: 1 file")
	require.NotContains(t, header, "+100000")
	require.NotContains(t, header, "edited main.go")
	require.Len(t, g.Changes(), 1, "the file remains available in the review")
	require.Nil(t, result.Review.Changes[0].Transfer, "presentation does not rewrite saved history")
}
