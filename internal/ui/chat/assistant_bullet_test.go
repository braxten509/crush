package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// Each reply opens with a "•" in the gutter (column 0), like Codex; the rest
// of the reply, thinking included, keeps the plain two-column indent.
func TestAssistantReplyOpensWithBullet(t *testing.T) {
	t.Parallel()

	sty := styles.CharmtonePantera()
	for _, tt := range []struct {
		name     string
		parts    []message.ContentPart
		wantText string
	}{
		{"plain reply", []message.ContentPart{
			message.TextContent{Text: "First line\n\nSecond paragraph"},
		}, "First line"},
		{"reply after thinking", []message.ContentPart{
			message.ReasoningContent{Thinking: "pondering"},
			message.TextContent{Text: "Answer here"},
		}, "Answer here"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			parts := append(tt.parts, message.Finish{Reason: message.FinishReasonEndTurn, Time: 1})
			m := &message.Message{ID: tt.name, Role: message.Assistant, Parts: parts}
			item := NewAssistantMessageItem(&sty, m).(*AssistantMessageItem)

			lines := strings.Split(ansi.Strip(item.Render(80)), "\n")
			bullets := 0
			for _, line := range lines {
				if strings.HasPrefix(line, "• ") {
					bullets++
					require.Equal(t, "• "+tt.wantText, strings.TrimRight(line, " "))
				} else if line != "" {
					require.True(t, strings.HasPrefix(line, "  "), "unbulleted line keeps the indent: %q", line)
				}
			}
			require.Equal(t, 1, bullets, "exactly one bullet per reply:\n%s", strings.Join(lines, "\n"))
		})
	}
}

// A reply with no text (only tool calls, or still thinking) has no bullet.
func TestAssistantWithoutTextHasNoBullet(t *testing.T) {
	t.Parallel()

	sty := styles.CharmtonePantera()
	m := &message.Message{ID: "think", Role: message.Assistant, Parts: []message.ContentPart{
		message.ReasoningContent{Thinking: "pondering"},
		message.Finish{Reason: message.FinishReasonEndTurn, Time: 1},
	}}
	item := NewAssistantMessageItem(&sty, m).(*AssistantMessageItem)
	require.NotContains(t, ansi.Strip(item.Render(80)), "•")
}
