package model

import (
	"strings"
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
	id := u.queue.pending[0].ID
	u.applyPromptQueue(promptQueueMsg{
		forSession: "s1", gen: u.queue.gen,
		prompts: []string{"later"}, entries: []message.QueuedPromptSummary{{Prompt: "later", SubmissionID: id}},
	})
	require.Equal(t, []string{"later"}, u.visiblePromptQueueItems(), "listed once the agent reports it")
	require.NotContains(t, ansi.Strip(u.chat.list.Render()), "later")

	saved := u.queue.pending[0].Clone()
	saved.ID = "saved"
	u.appendSessionMessage(saved)
	require.Len(t, u.chat.flat, 1)
	require.Equal(t, "saved", u.chat.flat[0].ID())
	require.Empty(t, u.visiblePromptQueueItems(), "the started prompt leaves the queue list")
	require.Empty(t, u.queue.held)
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

	saved := u.queue.pending[0].Clone()
	saved.ID = "saved-first"
	u.appendSessionMessage(saved)
	require.Len(t, u.chat.flat, 2, "the saved copy replaces the released row")
	require.Equal(t, "saved-first", u.chat.flat[0].ID())
	require.Equal(t, u.queue.pending[0].ID, u.chat.flat[1].ID(), "the other released row keeps its own ID")
}

// A held prompt's text shows right above the composer, so a message sent
// mid-turn never looks like it vanished.
func TestHeldPromptTextIsListedAboveComposer(t *testing.T) {
	u, _ := newSubmissionUI()
	warmCaches(u, true)
	u.sendMessage("only send should ask")
	u.updateLayoutAndSize()
	require.Zero(t, u.pillsAreaHeight(), "queued messages are not part of the to-do section")
	rows := strings.Split(ansi.Strip(u.renderEditorView(60)), "\n")
	require.Contains(t, rows[1], "› only send should ask", "the queued row sits under the attachments row")
	require.Contains(t, rows[1], "waiting · sends next")
	require.Equal(t, ansi.StringWidth(rows[2]), ansi.StringWidth(rows[1]), "it spans the composer band's width")
	require.Equal(t, 3, u.composerTextTop(), "the composer text moves down one row")
	require.Equal(t, u.textarea.Height()+editorHeightMargin+1, u.layoutEditorHeight())
}
