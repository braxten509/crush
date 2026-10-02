package model

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/secureentry"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/stretchr/testify/require"
)

func TestApplyChatScroll_LargeDeltaIsNotRewoundBySelection(t *testing.T) {
	t.Parallel()
	u := newFrameTestUI(t)
	u.chat.ScrollToBottom()
	u.chat.SelectLast()
	before := u.chat.Offset()

	u.applyChatScroll(-60)
	require.Equal(t, before-60, u.chat.Offset(), "selection follow must not rewind the viewport")
	require.True(t, u.chat.SelectedItemInView())

	u.applyChatScroll(60)
	require.True(t, u.chat.AtBottom())
	require.Equal(t, u.chat.Len()-1, u.chat.Selected(), "reaching the bottom must select the last item")
}

func TestApplyChatScroll_OutOfViewSelectionGoesToNearestEdge(t *testing.T) {
	t.Parallel()
	u := newFrameTestUI(t)
	u.chat.ScrollToBottom()

	// Selection far above the viewport, user scrolls up: it should land on
	// the top row (nearest edge), not jump to the bottom row.
	u.chat.SetSelected(0)
	u.applyChatScroll(-5)
	got := u.chat.Selected()
	u.chat.SelectFirstInView()
	require.Equal(t, u.chat.Selected(), got, "selection above viewport must snap to the top edge")

	// Selection below the viewport, user scrolls down: bottom row.
	u.chat.ScrollToTop()
	u.chat.SetSelected(u.chat.Len() - 1)
	u.applyChatScroll(5)
	got = u.chat.Selected()
	u.chat.SelectLastInView()
	require.Equal(t, u.chat.Selected(), got, "selection below viewport must snap to the bottom edge")
}

// A release taken by a dialog or the secure entry must still end a
// scrollbar drag, or the next chat drag scrolls instead of selecting.
func TestScrollbarDragEndsWhenReleaseGoesToOverlay(t *testing.T) {
	t.Parallel()
	for name, open := range map[string]func(u *UI){
		"dialog": func(u *UI) {
			u.dialog.OpenDialog(dialog.NewPermissions(u.com, permission.PermissionRequest{ID: "p1", ToolCallID: "t1", ToolName: "bash"}))
		},
		"secure entry": func(u *UI) {
			u.Update(&secureentry.Request{Spec: secureentry.Spec{File: "/dummy/keys.env", Label: "Test key"}})
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			u := newFrameTestUI(t)
			u.View()
			handled, _ := u.chat.HandleScrollbarPress(u.chat.scrollbarEdge, 1)
			require.True(t, handled)
			require.True(t, u.chat.DraggingScrollbar())

			open(u)
			u.Update(tea.MouseReleaseMsg{X: 1, Y: 1, Button: tea.MouseLeft})
			require.False(t, u.chat.DraggingScrollbar())
		})
	}
}
