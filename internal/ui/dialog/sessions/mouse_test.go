package sessions

import (
	"context"
	"fmt"
	"image"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/crush/internal/workspace"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/stretchr/testify/require"
)

type sessionMouseWorkspace struct {
	workspace.Workspace
	sessions []session.Session
}

func (w *sessionMouseWorkspace) ListSessions(context.Context) ([]session.Session, error) {
	return w.sessions, nil
}

func (w *sessionMouseWorkspace) AgentIsReady() bool {
	return false
}

func newSessionMouseDialog(t *testing.T, sessions []session.Session, selectedSessionID string) *Session {
	t.Helper()

	sty := styles.CharmtonePantera()
	dlg, err := NewSessions(&common.Common{
		Workspace: &sessionMouseWorkspace{sessions: sessions},
		Styles:    &sty,
	}, selectedSessionID)
	require.NoError(t, err)
	return dlg
}

func TestSessionMouseWheelScrollsListUnderPointer(t *testing.T) {
	t.Parallel()

	sessions := make([]session.Session, 40)
	for i := range sessions {
		sessions[i] = session.Session{
			ID:    fmt.Sprintf("s%02d", i),
			Title: fmt.Sprintf("Session %02d", i),
		}
	}
	dlg := newSessionMouseDialog(t, sessions, "s00")

	scr := uv.NewScreenBuffer(80, 30)
	dlg.Draw(scr, image.Rect(0, 0, 80, 30))
	require.Zero(t, dlg.list.Offset())

	dlg.HandleMsg(common.CoalescedWheelMsg{
		Mouse:  tea.Mouse{X: 10, Y: 10},
		DeltaY: 3,
	})
	require.Equal(t, 3, dlg.list.Offset())
	require.Equal(t, "s00", dlg.selectedSessionItem().ID())

	// Rendering must preserve the offset chosen by the wheel event.
	dlg.Draw(scr, image.Rect(0, 0, 80, 30))
	require.Equal(t, 3, dlg.list.Offset())

	// Wheel movement outside the rendered list has no effect.
	dlg.HandleMsg(common.CoalescedWheelMsg{
		Mouse:  tea.Mouse{X: 0, Y: 0},
		DeltaY: 3,
	})
	require.Equal(t, 3, dlg.list.Offset())
}

func TestSessionMouseClickHighlightsEntry(t *testing.T) {
	t.Parallel()

	dlg := newSessionMouseDialog(t, []session.Session{
		{ID: "s1", Title: "First"},
		{ID: "s2", Title: "Second"},
		{ID: "s3", Title: "Third"},
	}, "s1")
	scr := uv.NewScreenBuffer(80, 30)
	dlg.Draw(scr, image.Rect(0, 0, 80, 30))
	listArea := dlg.sessionListArea()
	require.False(t, listArea.Empty())

	action := dlg.HandleMsg(tea.MouseClickMsg(tea.Mouse{
		X:      listArea.Min.X,
		Y:      listArea.Min.Y + 1,
		Button: tea.MouseLeft,
	}))

	require.Nil(t, action)
	require.Equal(t, "s2", dlg.selectedSessionItem().ID())
}

func TestSessionMouseDoubleClickOpensEntryLikeEnter(t *testing.T) {
	t.Parallel()

	dlg := newSessionMouseDialog(t, []session.Session{
		{ID: "s1", Title: "First"},
		{ID: "s2", Title: "Second"},
	}, "s1")
	scr := uv.NewScreenBuffer(80, 30)
	dlg.Draw(scr, image.Rect(0, 0, 80, 30))
	listArea := dlg.sessionListArea()
	click := tea.MouseClickMsg(tea.Mouse{
		X:      listArea.Min.X,
		Y:      listArea.Min.Y + 1,
		Button: tea.MouseLeft,
	})

	require.Nil(t, dlg.HandleMsg(click))
	enterAction := dlg.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	doubleClickAction := dlg.HandleMsg(click)

	require.Equal(t, enterAction, doubleClickAction)
	selected, ok := doubleClickAction.(dialog.ActionSelectSession)
	require.True(t, ok)
	require.Equal(t, "s2", selected.Session.ID)
}

func TestSessionMouseDoubleClickExpires(t *testing.T) {
	t.Parallel()

	dlg := newSessionMouseDialog(t, []session.Session{
		{ID: "s1", Title: "First"},
		{ID: "s2", Title: "Second"},
	}, "s1")
	scr := uv.NewScreenBuffer(80, 30)
	dlg.Draw(scr, image.Rect(0, 0, 80, 30))
	listArea := dlg.sessionListArea()
	click := tea.MouseClickMsg(tea.Mouse{
		X:      listArea.Min.X,
		Y:      listArea.Min.Y + 1,
		Button: tea.MouseLeft,
	})

	require.Nil(t, dlg.HandleMsg(click))
	dlg.lastClickTime = time.Now().Add(-sessionDoubleClickThreshold - time.Millisecond)
	require.Nil(t, dlg.HandleMsg(click))
	require.Equal(t, "s2", dlg.selectedSessionItem().ID())
}

func TestSessionMouseClickSelectsFirstVisibleEntryAfterScroll(t *testing.T) {
	t.Parallel()

	sessions := make([]session.Session, 40)
	for i := range sessions {
		sessions[i] = session.Session{
			ID:    fmt.Sprintf("s%02d", i),
			Title: fmt.Sprintf("Session %02d", i),
		}
	}
	dlg := newSessionMouseDialog(t, sessions, "s00")
	scr := uv.NewScreenBuffer(80, 30)
	dlg.Draw(scr, image.Rect(0, 0, 80, 30))
	listArea := dlg.sessionListArea()
	dlg.HandleMsg(common.CoalescedWheelMsg{
		Mouse:  tea.Mouse{X: listArea.Min.X, Y: listArea.Min.Y},
		DeltaY: 3,
	})
	require.Equal(t, 3, dlg.list.Offset())

	action := dlg.HandleMsg(tea.MouseClickMsg(tea.Mouse{
		X:      listArea.Min.X,
		Y:      listArea.Min.Y,
		Button: tea.MouseLeft,
	}))

	require.Nil(t, action)
	require.Equal(t, "s03", dlg.selectedSessionItem().ID())
}

func TestSessionMouseClickIgnoresUnsupportedClicks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		point  func(image.Rectangle) image.Point
		button tea.MouseButton
		mode   sessionsMode
	}{
		{
			name:   "outside list",
			point:  func(image.Rectangle) image.Point { return image.Pt(0, 0) },
			button: tea.MouseLeft,
			mode:   sessionsModeNormal,
		},
		{
			name:   "right button",
			point:  func(area image.Rectangle) image.Point { return image.Pt(area.Min.X, area.Min.Y+1) },
			button: tea.MouseRight,
			mode:   sessionsModeNormal,
		},
		{
			name:   "deleting mode",
			point:  func(area image.Rectangle) image.Point { return image.Pt(area.Min.X, area.Min.Y+1) },
			button: tea.MouseLeft,
			mode:   sessionsModeDeleting,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dlg := newSessionMouseDialog(t, []session.Session{
				{ID: "s1", Title: "First"},
				{ID: "s2", Title: "Second"},
			}, "s1")
			scr := uv.NewScreenBuffer(80, 30)
			dlg.Draw(scr, image.Rect(0, 0, 80, 30))
			dlg.sessionsMode = tt.mode
			point := tt.point(dlg.sessionListArea())

			action := dlg.HandleMsg(tea.MouseClickMsg(tea.Mouse{
				X:      point.X,
				Y:      point.Y,
				Button: tt.button,
			}))

			require.Nil(t, action)
			require.Equal(t, "s1", dlg.selectedSessionItem().ID())
		})
	}
}

var _ workspace.Workspace = (*sessionMouseWorkspace)(nil)
