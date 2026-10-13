package app

import (
	"bytes"
	"testing"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/noninteractive"
	"github.com/stretchr/testify/require"
)

func textMessage(id, text string) message.Message {
	return message.Message{
		ID:    id,
		Role:  message.Assistant,
		Parts: []message.ContentPart{message.TextContent{Text: text}},
	}
}

func TestReplyPrinter(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	p := replyPrinter{out: &noninteractive.Output{Stdout: &buf}, read: map[string]int{}}

	// Leading spaces and tabs of a message's first chunk are trimmed, and
	// whitespace-only output is skipped until there is text. Only the first
	// chunk is trimmed, so the tab here survives.
	require.NoError(t, p.print(textMessage("m1", "  ")))
	require.Empty(t, buf.String())
	require.NoError(t, p.print(textMessage("m1", "  \tHello")))
	require.NoError(t, p.print(textMessage("m1", "  \tHello, world")))
	// Re-printing the same content writes nothing.
	require.NoError(t, p.print(textMessage("m1", "  \tHello, world")))
	// A new message is separated by a blank line.
	require.NoError(t, p.print(textMessage("m2", "Bye")))
	require.Equal(t, "\tHello, world\n\nBye", buf.String())

	// Content that shrinks below what was printed is an error.
	require.ErrorContains(t, p.print(textMessage("m2", "B")), "message content is shorter than read bytes: 1 < 3")
}
