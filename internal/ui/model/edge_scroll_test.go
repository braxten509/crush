package model

import (
	"fmt"
	"image"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/stretchr/testify/require"
)

// A chat taller than its view, pinned to the newest message.
func newScrolledChatUI(t *testing.T) *UI {
	t.Helper()
	u := newTestUI()
	u.dialog = dialog.NewOverlay()
	u.layout.main = image.Rect(0, 0, 60, 10)
	u.chat.SetSize(u.layout.main.Dx(), u.layout.main.Dy())
	var items []chat.MessageItem
	for i := range 30 {
		items = append(items, chat.NewAssistantMessageItem(u.com.Styles, &message.Message{
			ID:    fmt.Sprintf("m%d", i),
			Role:  message.Assistant,
			Parts: []message.ContentPart{message.TextContent{Text: fmt.Sprintf("Reply %d", i)}},
		}))
	}
	u.chat.SetMessages(items...)
	u.chat.ScrollToBottom()
	require.True(t, u.chat.Follow())
	return u
}

// Moving the mouse over the chat's top or bottom row with no button held
// must not scroll: it would stop following, and new replies would land out
// of view.
func TestMouseMoveOverChatEdgeKeepsFollowing(t *testing.T) {
	t.Parallel()
	u := newScrolledChatUI(t)
	u.Update(tea.MouseMotionMsg{X: 5, Y: 0})
	u.Update(tea.MouseMotionMsg{X: 5, Y: u.chat.Height() - 1})
	require.True(t, u.chat.Follow())
	require.True(t, u.chat.AtBottom())
}

// Dragging a selection to the top edge still scrolls up.
func TestDragToChatTopEdgeStillScrolls(t *testing.T) {
	t.Parallel()
	u := newScrolledChatUI(t)
	u.Update(tea.MouseMotionMsg{X: 5, Y: 0, Button: tea.MouseLeft})
	require.False(t, u.chat.Follow())
	require.False(t, u.chat.AtBottom())
}
