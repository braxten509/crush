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
	require.Equal(t, 3, end)
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

func TestTreeFiltersFoldingRootForkAndSummaryDefault(t *testing.T) {
	style := styles.CharmtonePantera()
	com := &common.Common{Styles: &style}
	d := NewTree(com, []message.TreeEntry{
		{TreeNode: db.TreeNode{MessageID: "user"}, Message: message.Message{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "prompt"}}}},
		{TreeNode: db.TreeNode{MessageID: "reply", ParentID: "user", Label: "label", Active: true}, Message: message.Message{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "reply"}}}},
		{TreeNode: db.TreeNode{MessageID: "tool", ParentID: "reply"}, Message: message.Message{Role: message.Tool}},
	})
	require.Len(t, d.list.FilteredItems(), 3)
	d.list.SetSelected(0)
	require.Equal(t, ActionTreeJump{""}, d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter}))
	d.list.SetSelected(1)
	d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyLeft})
	require.Len(t, d.list.FilteredItems(), 2)
	d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyRight})
	require.Len(t, d.list.FilteredItems(), 3)
	d.filter = 4
	d.rebuild()
	require.Len(t, d.list.FilteredItems(), 4)
	d.filter = 3
	d.rebuild()
	require.Len(t, d.list.FilteredItems(), 2)
	d.Confirm("reply", "")
	require.Equal(t, ActionTreeNavigate{MessageID: "reply"}, d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter}))
	d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyRight})
	require.Equal(t, ActionTreeNavigate{MessageID: "reply", Summary: true}, d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter}))
	d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEscape})
	require.False(t, d.confirming)
	d.SetFork()
	require.Len(t, d.list.FilteredItems(), 1)
	require.Equal(t, ActionTreeCopy{"user", true}, d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter}))
	for _, size := range []image.Point{{40, 12}, {80, 24}, {120, 40}} {
		d.Draw(uv.NewScreenBuffer(size.X, size.Y), image.Rect(0, 0, size.X, size.Y))
	}
}
