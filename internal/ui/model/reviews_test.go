package model

import (
	"context"
	"testing"

	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/stretchr/testify/require"
)

type reviewWorkspace struct {
	testWorkspace
	reads   int
	session string
}

func (w *reviewWorkspace) LoadMessageReview(_ context.Context, sessionID, messageID string) (message.Message, error) {
	w.reads++
	w.session = sessionID
	return message.Message{ID: messageID, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "call", Name: "bash", Review: &filechange.Review{Changes: []filechange.Change{{Path: "/workspace/file", After: &filechange.State{Content: "new\n"}}}}}}}, nil
}

func TestReviewLoadsOffThreadAndDoesNotReopenDismissedDrawer(t *testing.T) {
	u := newTestUI()
	ws := &reviewWorkspace{}
	u.com.Workspace = ws
	u.session = &session.Session{ID: "parent"}
	u.dialog = dialog.NewOverlay()
	g := chat.NewToolGroupItem(u.com.Styles)
	call := message.ToolCall{ID: "call", Name: "bash", Finished: true}
	result := &message.ToolResult{ToolCallID: "call", Name: "bash", Review: &filechange.Review{ID: "result", SessionID: "child", Summary: &filechange.ReviewSummary{Files: 1, Paths: []string{"/workspace/file"}}}}
	g.SetChildren([]chat.MessageItem{chat.NewToolMessageItem(u.com.Styles, "result", call, result, false, "")})
	cmd := u.openReview(g)
	require.Zero(t, ws.reads, "opening the drawer does no IO in Update")
	require.NotNil(t, u.dialog.Dialog(dialog.ChangesID))
	loaded := cmd().(reviewLoadedMsg)
	require.NoError(t, loaded.err)
	require.Equal(t, "child", ws.session)
	require.Len(t, loaded.files, 1)
	u.dialog.CloseDialog(dialog.ChangesID)
	require.Nil(t, u.applyReview(loaded))
	require.False(t, u.dialog.HasDialogs())
}
