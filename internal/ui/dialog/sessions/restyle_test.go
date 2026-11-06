package sessions

import (
	"testing"

	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/dialog/internal/kittest"
	"github.com/stretchr/testify/require"
)

var (
	_ dialog.StyleRefresher = (*Session)(nil)
	_ dialog.StyleRefresher = (*SessionItem)(nil)
)

func TestRefreshStyles_Sessions(t *testing.T) {
	t.Parallel()
	ws := &sessionMouseWorkspace{sessions: []session.Session{
		{ID: "s1", Title: "First session"},
		{ID: "s2", Title: "Second session"},
	}}
	kittest.RequireRestyled(t, func(com *common.Common) *Session {
		com.Workspace = ws
		d, err := NewSessions(com, "s1")
		require.NoError(t, err)
		return d
	})
}
