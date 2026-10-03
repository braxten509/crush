package model

import (
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/stretchr/testify/require"
)

// An info row shows only under a reply it labels: not after the
// compaction marker, another info row, or a prompt.
func TestAssistantInfoHiddenWithoutReply(t *testing.T) {
	m := newPrismTestUI()
	sty, cfg := m.com.Styles, m.com.Config()
	turn := func(id, text string, summary bool) *message.Message {
		msg := &message.Message{ID: id, Role: message.Assistant, IsSummaryMessage: summary, Model: "m", Provider: "p"}
		if text != "" {
			msg.AppendContent(text)
		}
		msg.AddFinish(message.FinishReasonEndTurn, "", "")
		return msg
	}
	info := func(msg *message.Message) chat.MessageItem {
		return chat.NewAssistantInfoItem(sty, msg, cfg, time.Now())
	}
	user := &message.Message{ID: "u1", Role: message.User}
	user.AppendContent("go crazy")

	summary := turn("sum", "summary text", true)
	empty := turn("empty", "", false)
	reply := turn("reply", "done", false)
	m.chat.AppendMessages(chat.NewAssistantMessageItem(sty, summary), info(summary))
	m.chat.AppendMessages(info(empty))
	m.chat.AppendMessages(chat.NewUserMessageItem(sty, user, nil), info(empty))
	m.chat.AppendMessages(chat.NewAssistantMessageItem(sty, reply), info(reply))

	var shown []string
	for i := range m.chat.list.Len() {
		shown = append(shown, m.chat.list.ItemAt(i).(chat.MessageItem).ID())
	}
	require.Equal(t, []string{"sum", "u1", "reply", chat.AssistantInfoID("reply")}, shown)
	// Hidden rows stay findable so updates don't add them again.
	require.NotNil(t, m.chat.MessageItem(chat.AssistantInfoID("sum")))
	require.NotNil(t, m.chat.MessageItem(chat.AssistantInfoID("empty")))
}
