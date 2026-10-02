package model

import (
	"testing"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestReasoningRowDisappearsAndLaterReplyStillAppears(t *testing.T) {
	m := newPrismTestUI()
	msg := message.Message{ID: "thought", SessionID: "s1", Role: message.Assistant}
	msg.AppendReasoningContent("PRIVATE_REASONING_FIXTURE")
	_ = m.appendSessionMessage(msg)
	require.NotNil(t, m.chat.MessageItem(msg.ID), "live progress remains")
	msg.AddFinish(message.FinishReasonEndTurn, "", "")
	_ = m.updateSessionMessage(msg)
	require.Nil(t, m.chat.MessageItem(msg.ID), "finished thoughts do not leave an empty row")
	msg.AppendContent("Visible reply after an update.")
	_ = m.updateSessionMessage(msg)
	item := m.chat.MessageItem(msg.ID)
	require.NotNil(t, item)
	rendered := ansi.Strip(item.Render(80))
	require.Contains(t, rendered, "Visible reply after an update.")
	require.NotContains(t, rendered, "PRIVATE_REASONING_FIXTURE")
}
