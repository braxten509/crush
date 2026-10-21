package server

import (
	"testing"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

func TestPageMessages(t *testing.T) {
	t.Parallel()
	// u0 a1 t2 a3 u4 a5 t6 a7 u8 a9
	var msgs []message.Message
	for i, role := range []message.MessageRole{message.User, message.Assistant, message.Tool, message.Assistant, message.User,
		message.Assistant, message.Tool, message.Assistant, message.User, message.Assistant} {
		msgs = append(msgs, message.Message{ID: string(rune('0' + i)), Role: role})
	}
	ids := func(ms []message.Message) string {
		s := ""
		for _, m := range ms {
			s += m.ID
		}
		return s
	}
	require.Equal(t, "0123456789", ids(pageMessages(msgs, 0, "")), "no options: every message")
	require.Equal(t, "89", ids(pageMessages(msgs, 2, "")), "the last two start at a user message")
	require.Equal(t, "456789", ids(pageMessages(msgs, 5, "")), "a page reaches back to the user message before it")
	require.Equal(t, "0123", ids(pageMessages(msgs, 3, "4")), "an older page ends before the given message")
	require.Equal(t, "", ids(pageMessages(msgs, 3, "0")), "nothing is older than the first message")
	require.Empty(t, pageMessages(msgs, 3, "nope"), "an unknown message gives nothing")
	// No user message near: the page keeps its size rather than reading everything.
	tools := []message.Message{{ID: "a", Role: message.User}}
	for range 20 {
		tools = append(tools, message.Message{ID: "t", Role: message.Tool})
	}
	require.Len(t, pageMessages(tools, 3, ""), 3)
}
