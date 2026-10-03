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

// Ctrl+Enter moves a held prompt into the chat; its saved copy must then
// take that row's place rather than show the prompt twice.
func TestReleasedPromptIsReplacedBySavedCopy(t *testing.T) {
	u, _ := newSubmissionUI()
	warmCaches(u, true)
	u.chat.SetSize(100, 30)
	u.sendMessage("first")
	u.sendMessage("second")
	u.releaseHeldPrompts()
	require.Len(t, u.chat.flat, 2)

	saved := u.pendingPrompts[0].Clone()
	saved.ID = "saved-first"
	u.appendSessionMessage(saved)
	require.Len(t, u.chat.flat, 2, "the saved copy replaces the released row")
	require.Equal(t, "saved-first", u.chat.flat[0].ID())
	require.Equal(t, u.pendingPrompts[0].ID, u.chat.flat[1].ID(), "the other released row keeps its own ID")
}

// A held prompt's text shows above the composer without opening the panel,
// so a message sent mid-turn never looks like it vanished.
func TestHeldPromptTextIsListedWithoutExpanding(t *testing.T) {
	u, _ := newSubmissionUI()
	warmCaches(u, true)
	u.sendMessage("only send should ask")
	require.False(t, u.pillsExpanded)
	u.updateLayoutAndSize()
	require.Contains(t, ansi.Strip(u.pillsView), "only send should ask")
	require.Equal(t, pillHeightWithBorder+1, u.pillsAreaHeight())
}
