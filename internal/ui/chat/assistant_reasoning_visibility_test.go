package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// The legacy format/cache tests exercise the low-level reasoning renderer
// explicitly. Normal chat construction always uses the hidden policy.
func reasoningRendererFixture(sty *styles.Styles, msg *message.Message) MessageItem {
	return newAssistantMessageItem(sty, msg, false)
}

func TestChatHidesReasoningForEveryProvider(t *testing.T) {
	t.Parallel()
	for _, provider := range []string{"grok-cli", "codex-cli", "claude-code", "opencode-cli", "agy-cli", "abacus", "future-provider"} {
		t.Run(provider, func(t *testing.T) {
			sty := styles.CharmtonePantera()
			msg := &message.Message{ID: "visibility", Role: message.Assistant, Provider: provider}
			msg.AppendReasoningContent("PRIVATE_REASONING_FIXTURE")
			item := NewAssistantMessageItem(&sty, msg).(*AssistantMessageItem)
			first := ansi.Strip(item.Render(80))
			require.NotContains(t, first, "PRIVATE_REASONING_FIXTURE")
			require.Contains(t, first, "Thinking", "live status stays visible")
			require.Empty(t, item.thinkingSec.out, "hidden reasoning is never rendered")
			require.False(t, item.ToggleExpanded(), "keyboard expansion must not expose thoughts")
			require.False(t, item.HandleMouseClick(ansi.MouseLeft, 4, 0))

			msg.AppendContent("Visible reply.")
			msg.AddFinish(message.FinishReasonEndTurn, "", "")
			item.SetMessage(msg)
			for _, width := range []int{40, 120} {
				rendered := ansi.Strip(item.Render(width))
				require.Contains(t, rendered, "Visible reply.")
				require.NotContains(t, rendered, "PRIVATE_REASONING_FIXTURE")
				require.NotContains(t, rendered, "Thought for")
			}
			require.Contains(t, msg.ReasoningContent().Thinking, "PRIVATE_REASONING_FIXTURE", "presentation does not mutate the stored message")
			reopened := ExtractMessageItems(&sty, msg, nil, "")
			require.Len(t, reopened, 1)
			require.NotContains(t, ansi.Strip(reopened[0].Render(80)), "PRIVATE_REASONING_FIXTURE")
		})
	}
}

func TestFinishedReasoningOnlyMessagesDoNotLeaveRows(t *testing.T) {
	t.Parallel()
	sty := styles.CharmtonePantera()
	msg := &message.Message{ID: "thought", Role: message.Assistant}
	msg.AppendReasoningContent(strings.Repeat("hidden ", 100))
	require.True(t, ShouldRenderAssistantMessage(msg), "keep status while working")
	msg.AddFinish(message.FinishReasonEndTurn, "", "")
	require.False(t, ShouldRenderAssistantMessage(msg))
	require.Empty(t, ExtractMessageItems(&sty, msg, nil, ""))
}
