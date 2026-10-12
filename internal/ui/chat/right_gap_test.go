package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// Chat text keeps one empty column at the right edge, next to the
// scrollbar, while the user's band still reaches the edge.
func TestChatTextLeavesLastColumnEmpty(t *testing.T) {
	t.Parallel()
	text := strings.Repeat("a bb ccc ", 30)
	sty := styles.CharmtonePantera()
	for width := 40; width < 44; width++ {
		user := newTestUserItem(t, text)
		for _, line := range strings.Split(user.Render(width), "\n") {
			require.Equal(t, width, ansi.StringWidth(line), "the band fills the row")
			require.True(t, strings.HasSuffix(ansi.Strip(line), " "), "width %d: %q", width, ansi.Strip(line))
		}

		msg := &message.Message{ID: "reply", Role: message.Assistant}
		msg.AppendContent(text)
		msg.AddFinish(message.FinishReasonEndTurn, "", "")
		reply := NewAssistantMessageItem(&sty, msg)
		for _, line := range strings.Split(reply.Render(width), "\n") {
			plain := ansi.Strip(line)
			require.Less(t, ansi.StringWidth(strings.TrimRight(plain, " ")), width, "width %d: %q", width, plain)
		}
	}
}
