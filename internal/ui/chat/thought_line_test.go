package chat

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestAssistantBusyLineShowsThoughtLine(t *testing.T) {
	sty := styles.CharmtonePantera()
	msg := &message.Message{ID: "a", Role: message.Assistant, Activity: "thinking", ActivityAt: time.Now().Unix(),
		ActivityDetail: "**Checking how Magisk asks for su approval**"}
	item := NewAssistantMessageItem(&sty, msg).(*AssistantMessageItem)

	out := ansi.Strip(item.RawRender(120))
	require.Contains(t, out, "Thinking")
	require.Contains(t, out, " · Checking how Magisk asks for su approval")
	require.NotContains(t, out, "**")

	// A narrow window cuts the line instead of wrapping it.
	out = ansi.Strip(item.RawRender(40))
	require.NotContains(t, out, "\n")
	require.LessOrEqual(t, ansi.StringWidth(out), 40)
	require.True(t, strings.HasSuffix(out, "…"), out)

	// The setting turns it off.
	SetThoughtLineSetting(func() bool { return false })
	t.Cleanup(func() { thoughtLineSetting.Store(nil) })
	out = ansi.Strip(item.RawRender(120))
	require.Contains(t, out, "Thinking")
	require.NotContains(t, out, "Magisk")
}

func TestThoughtLineComesFromStreamedThinking(t *testing.T) {
	t.Parallel()
	msg := &message.Message{ID: "a", Role: message.Assistant}
	require.Empty(t, thoughtLine(msg), "no thinking yet")
	msg.StartReasoning("First step.\n\n## Second step\n")
	require.Equal(t, "Second step", thoughtLine(msg))
	msg.ActivityDetail = "Held line."
	require.Equal(t, "Held line.", thoughtLine(msg), "a CLI's held line wins")
	msg.ActivityDetail = ""
	msg.FinishThinking()
	require.Empty(t, thoughtLine(msg), "no line once the thinking ended")
}

func TestToolGroupShowsThoughtLineWhileThinking(t *testing.T) {
	t.Parallel()
	sty := styles.CharmtonePantera()
	g := NewToolGroupItem(&sty)
	g.live, g.busy = true, true
	call := message.ToolCall{ID: "t1", Name: "bash", Input: `{"command":"ls"}`, Finished: true}
	tool := NewToolMessageItem(&sty, "m1", call, nil, false, "")
	tool.SetResult(&message.ToolResult{ToolCallID: call.ID, Content: "done"})
	msg := &message.Message{ID: "m2", Role: message.Assistant, Activity: "thinking"}
	msg.StartReasoning("Look at the result.\nPick the next file to read.")
	g.SetChildren([]MessageItem{tool, NewAssistantMessageItem(&sty, msg)})
	header := ansi.Strip(g.header(200))
	require.Contains(t, header, "Thinking")
	require.Contains(t, header, " · Pick the next file to read.")

	running := message.ToolCall{ID: "t2", Name: "bash", Input: `{"command":"sleep 5"}`, Finished: true}
	g.SetChildren([]MessageItem{tool, NewAssistantMessageItem(&sty, msg), NewToolMessageItem(&sty, "m3", running, nil, false, "")})
	header = ansi.Strip(g.header(200))
	require.Contains(t, header, "Running a command")
	require.NotContains(t, header, "Pick the next file")
}

func TestAssistantWaitsForTheModel(t *testing.T) {
	t.Parallel()
	sty := styles.CharmtonePantera()
	msg := &message.Message{ID: "a", Role: message.Assistant}
	item := NewAssistantMessageItem(&sty, msg).(*AssistantMessageItem)
	require.Contains(t, ansi.Strip(item.renderSpinning()), "Waiting for the model")
}
