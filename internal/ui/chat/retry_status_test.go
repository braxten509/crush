package chat

import (
	"testing"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestRetryNoticeAppearsInChatAndFoldedActions(t *testing.T) {
	for _, test := range []struct{ activity, label string }{
		{"rate_limited", "Rate limited — waiting to retry"},
		{"retrying", "Waiting to retry"},
	} {
		t.Run(test.activity, func(t *testing.T) {
			sty := styles.CharmtonePantera()
			msg := &message.Message{ID: "waiting", Role: message.Assistant, Activity: test.activity}
			item := NewAssistantMessageItem(&sty, msg).(*AssistantMessageItem)
			require.Contains(t, ansi.Strip(item.Render(80)), test.label)
			group := NewToolGroupItem(&sty)
			group.live, group.busy = true, true
			group.SetChildren([]MessageItem{item})
			require.Contains(t, ansi.Strip(group.header(120)), test.label)

			msg.Activity = "thinking"
			item.SetMessage(msg)
			require.NotContains(t, ansi.Strip(item.Render(80)), test.label)
			require.NotContains(t, ansi.Strip(group.header(120)), test.label)
			msg.Activity = test.activity
			msg.AddFinish(message.FinishReasonCanceled, "", "")
			item.SetMessage(msg)
			require.NotContains(t, ansi.Strip(item.Render(80)), test.label)
			require.NotContains(t, ansi.Strip(group.header(120)), test.label)
		})
	}
}
