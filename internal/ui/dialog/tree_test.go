package dialog

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/stretchr/testify/require"
	"image"
	"testing"
)

func TestTreeSelectSearchAndBookmark(t *testing.T) {
	style := styles.CharmtonePantera()
	com := &common.Common{Styles: &style}
	d := NewTree(com, []message.TreeEntry{
		{TreeNode: db.TreeNode{MessageID: "root"}, Message: message.Message{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "alpha"}}}},
		{TreeNode: db.TreeNode{MessageID: "old", ParentID: "root", Active: true}, Message: message.Message{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "beta"}}}},
		{TreeNode: db.TreeNode{MessageID: "new", ParentID: "root"}, Message: message.Message{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "gamma"}}}},
	})
	screen := uv.NewScreenBuffer(100, 36)
	d.Draw(screen, image.Rect(0, 0, 100, 36))
	start, end := d.list.VisibleItemIndices()
	require.Equal(t, 0, start)
	require.Equal(t, 2, end)
	d.HandleMsg(struct{}{}) // unrelated background events must not reset selection
	require.Equal(t, ActionTreeJump{"old"}, d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter}))
	for _, r := range "gamma" {
		d.HandleMsg(keyMsg(r))
	}
	require.Equal(t, ActionTreeJump{"new"}, d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter}))
	d.HandleMsg(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	for _, r := range "bookmark" {
		d.HandleMsg(keyMsg(r))
	}
	require.Equal(t, ActionTreeLabel{"new", "bookmark"}, d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter}))
}
