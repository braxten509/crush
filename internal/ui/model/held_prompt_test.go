package model

import (
	"testing"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// A prompt sent mid-turn waits in the queue list, not the chat, until its
// turn starts and its saved copy arrives.
func TestMidTurnPromptWaitsInQueueList(t *testing.T) {
	u, _ := newSubmissionUI()
	warmCaches(u, true)
	u.chat.SetSize(100, 30)
	u.sendMessage("later")
	require.Empty(t, u.chat.flat)
	require.Equal(t, []string{"later"}, u.visiblePromptQueueItems(), "listed before the agent's queue is fetched")
	id := u.pendingPrompts[0].ID
	u.applyPromptQueue(promptQueueMsg{forSession: "s1", gen: u.promptQueueGen,
		prompts: []string{"later"}, entries: []message.QueuedPromptSummary{{Prompt: "later", SubmissionID: id}}})
	require.Equal(t, []string{"later"}, u.visiblePromptQueueItems(), "listed once the agent reports it")
	require.NotContains(t, ansi.Strip(u.chat.list.Render()), "later")

	saved := u.pendingPrompts[0].Clone()
	saved.ID = "saved"
	u.appendSessionMessage(saved)
	require.Len(t, u.chat.flat, 1)
	require.Equal(t, "saved", u.chat.flat[0].ID())
	require.Empty(t, u.visiblePromptQueueItems(), "the started prompt leaves the queue list")
	require.Empty(t, u.heldPrompts)
}
