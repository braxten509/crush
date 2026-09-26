package chat

import (
	"testing"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
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
