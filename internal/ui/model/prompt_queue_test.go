package model

import (
	"testing"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

func pendingPrompt(id, sessionID, text string) message.Message {
	return message.Message{ID: id, SessionID: sessionID, Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: text, SubmissionID: id}}}
}

func TestPromptQueueTakeAndHold(t *testing.T) {
	t.Parallel()

	var q promptQueue
	q.pending = []message.Message{pendingPrompt("a", "s1", "one"), pendingPrompt("b", "s1", "two")}
	q.hold("b")
	require.Equal(t, []string{"two"}, q.unlistedHeld("s1"))
	require.Empty(t, q.unlistedHeld("s2"))

	q.entries = []message.QueuedPromptSummary{{SubmissionID: "b", Prompt: "two"}}
	require.Empty(t, q.unlistedHeld("s1"), "a listed prompt shows once")

	require.Nil(t, q.take(""))
	require.Nil(t, q.take("missing"))
	taken := q.take("b")
	require.NotNil(t, taken)
	require.Equal(t, "two", taken.Content().Text)
	require.False(t, q.held["b"])
	require.Len(t, q.pending, 1)
}

func TestPromptQueueTrackAndClear(t *testing.T) {
	t.Parallel()

	var q promptQueue
	id := q.track("", "s1", func() {}, false)
	require.NotEmpty(t, id)
	require.Equal(t, "x", q.track("x", "s2", func() {}, true))
	require.True(t, q.submittingFor("s1"))
	require.True(t, q.submittingFor("s2"))
	require.False(t, q.submittingFor("s3"))

	q.count, q.items = 2, []string{"a", "b"}
	gen := q.gen
	q.clear()
	require.Zero(t, q.count)
	require.Nil(t, q.items)
	require.False(t, q.checkedAt.IsZero())
	q.invalidate()
	require.Equal(t, gen+1, q.gen)

	q.recallDraft("s1", message.QueuedPrompt{Prompt: "draft"})
	q.recallDraft("s1", message.QueuedPrompt{Prompt: "again"})
	require.Len(t, q.recalledDrafts["s1"], 2)
}
