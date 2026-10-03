package model

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func renderedRow(view, text string) int {
	for i, line := range strings.Split(ansi.Strip(view), "\n") {
		if strings.Contains(line, text) {
			return i
		}
	}
	return -1
}

func TestCtrlEnterAcknowledgesImmediatelyWithoutMovingThePendingBatch(t *testing.T) {
	u, ws := newSubmissionUI()
	u.chat.SetSize(100, 30)
	u.appendSessionMessage(message.Message{ID: "active", SessionID: "s1", Role: message.Assistant, Parts: []message.ContentPart{
		message.ToolCall{ID: "running-tool", Name: "bash", Input: `{"command":"sleep 10"}`, Finished: true},
	}})
	warmCaches(u, true)
	u.sendMessage("first followup")
	u.sendMessage("second followup")
	u.promptQueue = 2
	u.promptQueueItems = []string{"first followup", "second followup"}
	u.promptQueueEntries = []message.QueuedPromptSummary{{Prompt: "first followup", SubmissionID: u.pendingPrompts[0].ID}, {Prompt: "second followup", SubmissionID: u.pendingPrompts[1].ID}}
	u.updateLayoutAndSize()
	require.Equal(t, []string{"first followup", "second followup"}, u.visiblePromptQueueItems(), "mid-turn prompts wait in the queue list")
	require.Equal(t, -1, renderedRow(u.chat.list.Render(), "followup"), "and stay out of the chat")
	composer := u.layout.editor
	start := time.Now()
	cmd := u.handleKeyPressMsg(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl})
	elapsed := time.Since(start)
	t.Logf("Ctrl+Enter immediate update: %s", elapsed)
	require.Less(t, elapsed, 100*time.Millisecond)
	require.NotNil(t, cmd)
	require.Zero(t, ws.interruptCalls, "the key handler never blocks on a backend call")
	require.Equal(t, chat.ToolStatusCanceled, u.chat.MessageItem("running-tool").(chat.ToolMessageItem).Status())
	u.chat.ScrollToBottom()
	before := u.chat.list.Render()
	require.NotEqual(t, -1, renderedRow(before, "second followup"), "the interrupt sends the held prompts, so they join the chat")
	waiting := u.chat.waitingItem
	require.NotNil(t, waiting)
	require.Equal(t, composer, u.layout.editor)
	require.Len(t, u.pendingPrompts, 2)
	require.Len(t, u.submittingPrompts, 2, "queued handoffs must not be canceled with the old worker")
	require.Zero(t, u.visiblePromptQueueCount())
	for i := 0; i < 8; i++ {
		u.chat.Tick(animTickMsg{gen: u.chat.animGen})
		frame := u.chat.list.Render()
		require.Contains(t, ansi.Strip(frame), "Working")
		require.Equal(t, renderedRow(before, "first followup"), renderedRow(frame, "first followup"))
		require.Equal(t, renderedRow(before, "second followup"), renderedRow(frame, "second followup"))
		require.Equal(t, composer, u.layout.editor)
	}
	confirmed := u.pendingPrompts[len(u.pendingPrompts)-1].Clone()
	confirmed.ID = "saved-second"
	u.appendSessionMessage(confirmed)
	require.Same(t, waiting, u.chat.waitingItem, "persisted confirmation keeps the same animation")
	require.Equal(t, renderedRow(before, "second followup"), renderedRow(u.chat.list.Render(), "second followup"))
	late := message.Message{ID: "active", SessionID: "s1", Role: message.Assistant, Parts: []message.ContentPart{
		message.ToolCall{ID: "running-tool", Name: "bash", Input: `{"command":"sleep 10"}`, Finished: true},
		message.ToolCall{ID: "late-tool", Name: "bash", Input: `{"command":"echo late"}`, Finished: true},
	}}
	u.updateSessionMessage(late)
	require.Same(t, waiting, u.chat.waitingItem, "late old-turn updates cannot remove Working")
	require.Equal(t, renderedRow(before, "first followup"), renderedRow(u.chat.list.Render(), "first followup"))
	require.Equal(t, renderedRow(before, "second followup"), renderedRow(u.chat.list.Render(), "second followup"))
	require.Equal(t, composer, u.layout.editor)
	toolIndex, userIndex := -1, -1
	for i, item := range u.chat.flat {
		if item.ID() == "late-tool" {
			toolIndex = i
		}
		if item.ID() == "saved-second" {
			userIndex = i
		}
	}
	require.Less(t, toolIndex, userIndex, "late tool rows remain with the old turn")
	u.appendSessionMessage(message.Message{ID: "batch-response", SessionID: "s1", Role: message.Assistant})
	require.Nil(t, u.chat.waitingItem)
	require.Same(t, waiting, u.chat.MessageItem("batch-response"), "the response adopts the existing Working animation")
	require.Contains(t, ansi.Strip(u.chat.list.Render()), "Working")
	require.Nil(t, u.handleKeyPressMsg(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl}), "repeat presses cannot interrupt the next batch")
	require.IsType(t, agentInterruptedMsg{}, cmd())
	require.Equal(t, 1, ws.interruptCalls)
	u.Update(agentInterruptedMsg{sessionID: "s1"})
	require.Nil(t, u.handleKeyPressMsg(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl, IsRepeat: true}), "held keys cannot cancel the replacement after acknowledgement")
	require.Equal(t, 1, ws.interruptCalls)
}
