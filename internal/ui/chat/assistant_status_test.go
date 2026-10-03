package chat

import (
	"testing"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestAssistantStatusSurvivesCommentary(t *testing.T) {
	t.Parallel()
	sty := styles.CharmtonePantera()
	msg := &message.Message{ID: "progress", Role: message.Assistant}
	msg.AppendReasoningContent("Checking the changes.")
	item := NewAssistantMessageItem(&sty, msg).(*AssistantMessageItem)
	require.Contains(t, ansi.Strip(item.Render(80)), "Thinking")

	msg.AppendContent("I'm checking fresh and resumed chats.")
	item.SetMessage(msg)
	require.True(t, item.Spinning(), "commentary does not finish a turn")
	rendered := ansi.Strip(item.Render(80))
	require.Contains(t, rendered, "I'm checking fresh and resumed chats.")
	require.Contains(t, rendered, "Working")

	before := item.Version()
	require.True(t, item.Advance())
	require.Greater(t, item.Version(), before, "quiet periods must keep refreshing status")

	msg.Parts = append(msg.Parts, message.ToolCall{ID: "test", Name: "bash"})
	item.SetMessage(msg)
	require.False(t, item.Spinning(), "tool status takes over without a duplicate spinner")
}

func TestAssistantStatusStopsOnTerminalResult(t *testing.T) {
	t.Parallel()
	for _, reason := range []message.FinishReason{
		message.FinishReasonEndTurn, message.FinishReasonCanceled, message.FinishReasonError,
	} {
		t.Run(string(reason), func(t *testing.T) {
			t.Parallel()
			sty := styles.CharmtonePantera()
			msg := &message.Message{ID: "progress", Role: message.Assistant}
			msg.AppendContent("Checking the fix.")
			item := NewAssistantMessageItem(&sty, msg).(*AssistantMessageItem)
			require.True(t, item.Spinning())
			msg.Parts = append(msg.Parts, message.Finish{Reason: reason})
			item.SetMessage(msg)
			require.False(t, item.Spinning())
			require.True(t, item.Finished())
			require.NotContains(t, ansi.Strip(item.Render(80)), "Working")
		})
	}
}

func TestCompactingStatus(t *testing.T) {
	t.Parallel()
	for _, summary := range []bool{false, true} {
		sty := styles.CharmtonePantera()
		msg := &message.Message{ID: "compact", Role: message.Assistant, IsSummaryMessage: summary, IsCompacting: !summary, ActivityAt: 1}
		msg.AppendReasoningContent("Preparing context.")
		item := NewAssistantMessageItem(&sty, msg).(*AssistantMessageItem)
		require.Contains(t, ansi.Strip(item.Render(80)), "Compacting conversation")
		require.Regexp(t, `^Compacting conversation\.{1,3} *(?: \d+(?:s|m \d+s|h \d+m))?$`, ansi.Strip(item.renderSpinning()))
		require.False(t, Foldable(item), "compaction must stay visible outside tool groups")

		if !summary {
			msg.IsCompacting = false
			item.SetMessage(msg)
			require.NotContains(t, ansi.Strip(item.Render(80)), "Compacting conversation")
			require.Contains(t, ansi.Strip(item.Render(80)), "Thinking")
		}
		for _, reason := range []message.FinishReason{message.FinishReasonEndTurn, message.FinishReasonCanceled, message.FinishReasonError} {
			finished := msg.Clone()
			finished.IsCompacting = true
			finished.AddFinish(reason, "", "")
			item.SetMessage(&finished)
			require.False(t, finished.IsCompacting)
			require.False(t, item.Spinning())
			require.NotContains(t, ansi.Strip(item.Render(80)), "Compacting conversation")
		}
	}
}

func TestCompactionShowsOnlyStatus(t *testing.T) {
	t.Parallel()
	sty := styles.CharmtonePantera()
	for _, summary := range []bool{false, true} {
		msg := &message.Message{ID: "hidden-summary", Role: message.Assistant, IsSummaryMessage: summary, IsCompacting: !summary}
		msg.AppendContent("Internal summary content")
		msg.AppendReasoningContent("Internal summary reasoning")
		item := newAssistantMessageItem(&sty, msg, false).(*AssistantMessageItem)
		require.Regexp(t, `^Compacting conversation\. *$`, ansi.Strip(item.RawRender(100)))
		require.Empty(t, item.CopySource())
		if summary {
			msg.AddFinish(message.FinishReasonEndTurn, "", "")
			item.SetMessage(msg)
			require.Equal(t, "Conversation compacted", ansi.Strip(item.RawRender(100)))
			require.Empty(t, item.CopySource())
		}
	}
}
