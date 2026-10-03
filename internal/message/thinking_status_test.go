package message

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestThinkingStatusDoesNotRequireReasoningText(t *testing.T) {
	m := Message{Role: Assistant}
	m.AppendReasoningContent("")
	require.True(t, m.IsThinking())
	require.Empty(t, m.ReasoningContent().Thinking)
	m.FinishThinking()
	require.False(t, m.IsThinking())
}
