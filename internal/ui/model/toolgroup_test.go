package model

import (
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestChatGroupsToolRuns(t *testing.T) {
	t.Parallel()
	com := common.DefaultCommon(&channelWorkspace{})
	c := NewChat(com, config.ScrollbarDefault)
	tool := func(id string) chat.MessageItem {
		return chat.NewToolMessageItem(com.Styles, "m", message.ToolCall{ID: id, Name: tools.BashToolName, Input: `{"command":"ls"}`, Finished: true}, nil, false, "")
	}
	text := chat.NewAssistantMessageItem(com.Styles, &message.Message{ID: "a1", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "hi"}}})

	// Runs of tool calls fold into one group each; text stays on its own.
	c.SetMessages(tool("t1"), tool("t2"), text, tool("t3"))
	require.Equal(t, 3, c.Len())
	g, ok := c.list.ItemAt(0).(*chat.ToolGroupItem)
	require.True(t, ok)
	require.Len(t, g.Children(), 2)
	require.Equal(t, "t2", c.MessageItem("t2").ID(), "folded steps are still found by ID")

	// Dropping the text between two runs merges them into the first
	// group, which keeps its expanded state.
	require.True(t, g.ToggleExpanded())
	c.RemoveMessage("a1")
	require.Equal(t, 1, c.Len())
	require.Same(t, g, c.list.ItemAt(0))
	require.Len(t, g.Children(), 3)
	require.False(t, g.ToggleExpanded())
}

// When a thinking-only step ends without text and is removed as the agent
// goes idle, the group must re-render without the "Thinking" status.
func TestChatGroupRerendersAfterEmptyStepRemoved(t *testing.T) {
	u := newFrameTestUI(t)
	tc := message.ToolCall{ID: "t1", Name: "bash", Input: `{"command":"ls"}`, Finished: true}
	done := chat.NewToolMessageItem(u.com.Styles, "prev", tc, &message.ToolResult{ToolCallID: tc.ID, Content: "ok"}, false, "")
	msg := &message.Message{ID: "step", Role: message.Assistant}
	item := chat.NewAssistantMessageItem(u.com.Styles, msg).(*chat.AssistantMessageItem)
	u.chat.SetAgentBusy(true)
	u.chat.SetMessages(done, item)
	for _, d := range []string{"checking", " more"} {
		msg.AppendReasoningContent(d)
		item.SetMessage(msg)
		u.chat.Refold(item)
	}
	require.Contains(t, ansi.Strip(u.chat.list.Render()), "Thinking")

	msg.Parts = append(msg.Parts, message.Finish{Reason: message.FinishReasonEndTurn})
	item.SetMessage(msg)
	u.chat.Refold(item)
	u.chat.SetAgentBusy(false)
	require.False(t, chat.ShouldRenderAssistantMessage(msg))
	u.chat.RemoveMessage(msg.ID) // what updateSessionMessage does

	require.Nil(t, u.chat.MessageItem("step"))
	require.NotContains(t, ansi.Strip(u.chat.list.Render()), "Thinking")
}

// Only the end of the chat shows a live status. A step still marked as
// compacting with newer steps after it shows its text, and the live group
// at the end reports the compaction instead of "Thinking".
func TestChatShowsOneLiveStatus(t *testing.T) {
	u := newFrameTestUI(t)
	msg := &message.Message{ID: "step", Role: message.Assistant, IsCompacting: true}
	msg.AppendContent("All bridge tests pass so far.")
	step := chat.NewAssistantMessageItem(u.com.Styles, msg).(*chat.AssistantMessageItem)
	tc := message.ToolCall{ID: "t1", Name: "bash", Input: `{"command":"ls"}`, Finished: true}
	done := chat.NewToolMessageItem(u.com.Styles, "next", tc, &message.ToolResult{ToolCallID: tc.ID, Content: "ok"}, false, "")
	u.chat.SetAgentBusy(true)
	u.chat.SetMessages(step, done)

	out := ansi.Strip(u.chat.list.Render())
	require.Equal(t, 1, strings.Count(out, "Compacting conversation"), out)
	require.NotContains(t, out, "Thinking")
	require.Contains(t, out, "All bridge tests pass so far.")
	g, ok := u.chat.list.ItemAt(u.chat.Len() - 1).(*chat.ToolGroupItem)
	require.True(t, ok)
	require.Contains(t, ansi.Strip(g.Render(100)), "Compacting conversation")

	// Compaction ends: the group goes back to "Thinking".
	msg.IsCompacting = false
	step.SetMessage(msg)
	u.chat.Refold(step)
	out = ansi.Strip(u.chat.list.Render())
	require.NotContains(t, out, "Compacting conversation")
	require.Equal(t, 1, strings.Count(out, "Thinking"), out)
}
