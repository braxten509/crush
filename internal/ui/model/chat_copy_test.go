package model

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/stretchr/testify/require"
)

func TestChatSelectionCopiesCommandSource(t *testing.T) {
	code := "    bash /home/example/" + strings.Repeat("directory/", 15) + "install.sh \\\n      /home/example/.ssh/deploy-key"
	for _, width := range []int{40, 100, 160} {
		for _, backwards := range []bool{false, true} {
			t.Run(fmt.Sprintf("width%d/backwards%t", width, backwards), func(t *testing.T) {
				u := newTestUI()
				item := chat.NewAssistantMessageItem(u.com.Styles, &message.Message{
					ID: "command", Role: message.Assistant,
					Parts: []message.ContentPart{message.TextContent{Text: "```sh\n" + code + "\n```"}, message.Finish{Reason: message.FinishReasonEndTurn}},
				})
				u.chat.SetMessages(item)
				u.chat.list.SetSize(width, 100)
				height := lipgloss.Height(item.RawRender(width))
				u.chat.mouseDownItem, u.chat.mouseDragItem = 0, 0
				u.chat.mouseDownY, u.chat.mouseDownX = 0, 0
				u.chat.mouseDragY, u.chat.mouseDragX = height-1, width
				if backwards {
					u.chat.mouseDownY, u.chat.mouseDragY = u.chat.mouseDragY, u.chat.mouseDownY
					u.chat.mouseDownX, u.chat.mouseDragX = u.chat.mouseDragX, u.chat.mouseDownX
				}
				u.chat.applyHighlightRange(0, 0, item)
				require.Equal(t, code+"\n", u.chat.HighlightContent())
			})
		}
	}
}
