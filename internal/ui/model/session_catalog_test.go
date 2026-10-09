package model

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSavedSessionReopensItsOriginalFolder(t *testing.T) {
	u := newTestUIWithConfig(t, &config.Config{Options: &config.Options{DataDirectory: "/current/data"}})
	u.agentBusyCache.set(false)
	target := session.Session{ID: "saved", Title: "Saved chat", Directory: t.TempDir(), DataDirectory: t.TempDir()}
	cmd := u.openSession(target)
	require.NotNil(t, cmd)
	require.IsType(t, tea.QuitMsg{}, cmd())
	require.Equal(t, target.Directory, u.RelaunchDir())
	id, data := u.RelaunchSession()
	require.Equal(t, target.ID, id)
	require.Equal(t, target.DataDirectory, data)
}
