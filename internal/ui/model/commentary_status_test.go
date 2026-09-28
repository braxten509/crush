package model

import (
	"testing"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestCommentaryKeepsAnimationClockAlive(t *testing.T) {
	t.Parallel()
	u := newFrameTestUI(t)
	tc := message.ToolCall{ID: "test", Name: "bash", Input: `{"command":"go test ./..."}`, Finished: true}
	done := chat.NewToolMessageItem(u.com.Styles, "previous", tc,
		&message.ToolResult{ToolCallID: tc.ID, Content: "ok"}, false, "")
	msg := &message.Message{ID: "progress", Role: message.Assistant}
	msg.AppendContent("I'm checking fresh and resumed chats.")
	item := chat.NewAssistantMessageItem(u.com.Styles, msg).(*chat.AssistantMessageItem)
	u.chat.SetMessages(done, item)
	u.chat.SetAgentBusy(true)
	u.chat.ScrollToBottom()

	u.Update(neutralMsg{})
	require.True(t, u.chat.animRunning, "text after a finished tool group must keep status alive")
	require.Contains(t, ansi.Strip(u.chat.list.Render()), "Working")
	before := item.Version()
	for range 40 {
		u.Update(animTickMsg{gen: u.chat.animGen})
	}
	require.True(t, u.chat.animRunning, "animation must continue without provider events")
	require.Greater(t, item.Version(), before)

	msg.Parts = append(msg.Parts, message.Finish{Reason: message.FinishReasonEndTurn})
	item.SetMessage(msg)
	u.chat.SetAgentBusy(false)
	u.Update(animTickMsg{gen: u.chat.animGen})
	require.False(t, u.chat.animRunning)
	require.NotContains(t, ansi.Strip(u.chat.list.Render()), "Working")
}
