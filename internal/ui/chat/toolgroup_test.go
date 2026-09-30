package chat

import (
	"testing"

	"github.com/charmbracelet/crush/internal/message"
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
