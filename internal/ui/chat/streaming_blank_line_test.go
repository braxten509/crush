package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// A streamed reply ends up exactly as a one-piece render: gluing the
// stable prefix to the rest must not add a second blank line (glamour's
// blank margins are styled, so they only look empty) or drop a table's
// leading space.
func TestStreamedReplyMatchesFullRender(t *testing.T) {
	t.Parallel()

	text := "Done.\n\nTwo things to know:\n\n" +
		"- **It took the fixes from the branch, not from `main`.** More words here.\n" +
		"- **The server wasn't running.** It starts next time.\n\n" +
		"| Setting | Time |\n|---|---|\n| A | 1s |\n| B | 2s |\n\n" +
		"The models still find real bugs. The steps are in the README."
	sty := styles.CharmtonePantera()
	msg := func(s string, done bool) *message.Message {
		parts := []message.ContentPart{message.TextContent{Text: s}}
		if done {
			parts = append(parts, message.Finish{Reason: message.FinishReasonEndTurn})
		}
		return &message.Message{ID: "a", Role: message.Assistant, Parts: parts}
	}
	lines := func(s string) []string {
		out := strings.Split(ansi.Strip(s), "\n")
		for i, l := range out {
			out[i] = strings.TrimRight(l, " ")
		}
		return out
	}

	item := NewAssistantMessageItem(&sty, msg("", false)).(*AssistantMessageItem)
	for i := 7; i < len(text); i += 7 {
		item.SetMessage(msg(text[:i], false))
		item.RawRender(100)
	}
	item.SetMessage(msg(text, true))
	want := NewAssistantMessageItem(&sty, msg(text, true)).(*AssistantMessageItem).RawRender(100)
	require.Equal(t, lines(want), lines(item.RawRender(100)))
	require.NotContains(t, strings.Join(lines(item.RawRender(100)), "\n"), "know:\n\n\n")
}
