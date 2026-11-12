package model

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/stretchr/testify/require"
)

func TestCtrlEnterAcknowledgesImmediatelyWithoutMovingThePendingBatch(t *testing.T) {
	u, ws := newSubmissionUI()
	u.chat.SetSize(100, 30)
	u.appendSessionMessage(message.Message{ID: "active", SessionID: "s1", Role: message.Assistant, Parts: []message.ContentPart{
		message.ToolCall{ID: "running-tool", Name: "bash", Input: `{"command":"sleep 10"}`, Finished: true},
	}})
	warmCaches(u, true)
	u.sendMessage("first followup")
	u.sendMessage("second followup")
	u.queue.count = 2
	u.queue.items = []string{"first followup", "second followup"}
	u.queue.entries = []message.QueuedPromptSummary{{Prompt: "first followup", SubmissionID: u.queue.pending[0].ID}, {Prompt: "second followup", SubmissionID: u.queue.pending[1].ID}}
	u.updateLayoutAndSize()
	require.Equal(t, []string{"first followup", "second followup"}, u.visiblePromptQueueItems(), "mid-turn prompts wait in the queue list")
	for _, item := range u.chat.flat {
		for _, p := range u.queue.pending {
			require.NotEqual(t, p.ID, item.ID(), "and stay out of the chat")
		}
	}
	cmd := u.handleKeyPressMsg(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl})
	require.NotNil(t, cmd)
	require.Zero(t, ws.interruptCalls, "the key handler never blocks on a backend call")
	require.Equal(t, chat.ToolStatusCanceled, u.chat.MessageItem("running-tool").(chat.ToolMessageItem).Status())
	require.NotNil(t, u.chat.MessageItem(u.queue.pending[1].ID), "the interrupt sends the held prompts, so they join the chat")
	waiting := u.chat.waitingItem
	require.NotNil(t, waiting)
	require.Len(t, u.queue.pending, 2)
	require.Len(t, u.queue.submitting, 2, "queued handoffs must not be canceled with the old worker")
	require.Zero(t, u.visiblePromptQueueCount())
	confirmed := u.queue.pending[len(u.queue.pending)-1].Clone()
	confirmed.ID = "saved-second"
	u.appendSessionMessage(confirmed)
	require.Same(t, waiting, u.chat.waitingItem, "persisted confirmation keeps the same animation")
	late := message.Message{ID: "active", SessionID: "s1", Role: message.Assistant, Parts: []message.ContentPart{
		message.ToolCall{ID: "running-tool", Name: "bash", Input: `{"command":"sleep 10"}`, Finished: true},
		message.ToolCall{ID: "late-tool", Name: "bash", Input: `{"command":"echo late"}`, Finished: true},
	}}
	u.updateSessionMessage(late)
	require.Same(t, waiting, u.chat.waitingItem, "late old-turn updates cannot remove Working")
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
	require.Nil(t, u.handleKeyPressMsg(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl}), "repeat presses cannot interrupt the next batch")
	require.IsType(t, agentInterruptedMsg{}, cmd())
	require.Equal(t, 1, ws.interruptCalls)
	u.Update(agentInterruptedMsg{sessionID: "s1"})
	require.Nil(t, u.handleKeyPressMsg(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl, IsRepeat: true}), "held keys cannot cancel the replacement after acknowledgement")
	require.Equal(t, 1, ws.interruptCalls)
}
