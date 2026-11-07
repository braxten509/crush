package agent

import (
	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"context"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
	"testing"
)

type progressUpdateMessages struct{ message.Service }

func (progressUpdateMessages) Update(context.Context, message.Message) error { return nil }

func TestProgressUpdateStreamsVisibleTextAndUnblocksTool(t *testing.T) {
	msg := message.Message{Role: message.Assistant}
	ctx := t.Context()
	current := &turn{a: &sessionAgent{messages: progressUpdateMessages{}}, currentAssistant: &msg, genCtx: ctx,
		chatModel: Model{ModelCfg: config.SelectedModel{Provider: "anthropic", Model: "claude-opus-5-5"}},
		call:      SessionAgentCall{ProgressUpdateRequired: true}, toolCallLimit: 21, toolCallCount: 20}
	require.NoError(t, current.onReasoningStart("0", fantasy.ReasoningContent{}))
	require.NoError(t, current.onReasoningDelta("0", ""))
	require.Empty(t, msg.Content().Text)
	require.False(t, current.visibleAssistantText)
	require.NoError(t, current.onReasoningEnd("0", fantasy.ReasoningContent{ProviderMetadata: fantasy.ProviderMetadata{anthropic.Name: &anthropic.ReasoningOptionMetadata{Signature: "reasoning-signature"}}}))
	require.NoError(t, current.onReasoningStart("1", fantasy.ReasoningContent{}))
	require.NoError(t, current.onReasoningDelta("1", "Checked the fix. "))
	require.Contains(t, msg.Content().Text, "Checked the fix.", "must be visible before tool dispatch")
	require.NoError(t, current.onReasoningDelta("1", "Checking the next command."))
	require.NoError(t, current.onReasoningEnd("1", fantasy.ReasoningContent{ProviderMetadata: fantasy.ProviderMetadata{anthropic.Name: &anthropic.ReasoningOptionMetadata{Signature: "progress-signature"}}}))
	tool := &interruptProbeTool{}
	_, err := (progressInterruptTool{AgentTool: tool, turn: current}).Run(ctx, fantasy.ToolCall{Name: "work"})
	require.NoError(t, err)
	require.Equal(t, 1, tool.calls)
	require.Equal(t, 1, current.toolCallCount)
	require.NoError(t, current.onTextDelta("2", "Done."))
	replay := msg.ToAIMessage()[0].Content
	require.Len(t, replay, 3)
	note := replay[1].(fantasy.ReasoningPart)
	require.Equal(t, "Checked the fix. Checking the next command.", note.Text)
	require.Equal(t, "progress-signature", note.ProviderOptions[anthropic.Name].(*anthropic.ReasoningOptionMetadata).Signature)
	require.Equal(t, "Done.", replay[2].(fantasy.TextPart).Text, "progress is replayed only as its signed block")
}

func TestProgressUpdateLeavesOtherReasoningHidden(t *testing.T) {
	for _, provider := range []string{"openai", "openrouter"} {
		t.Run(provider, func(t *testing.T) {
			msg := message.Message{Role: message.Assistant}
			current := &turn{a: &sessionAgent{messages: progressUpdateMessages{}}, genCtx: t.Context(), currentAssistant: &msg,
				chatModel: Model{ModelCfg: config.SelectedModel{Provider: provider, Model: "claude-opus-5-5"}}}
			require.NoError(t, current.onReasoningStart("0", fantasy.ReasoningContent{}))
			require.NoError(t, current.onReasoningDelta("0", "Hidden reasoning"))
			require.Empty(t, msg.Content().Text)
			require.False(t, current.visibleAssistantText)
		})
	}
}
