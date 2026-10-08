package model

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/sessionhost"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/stretchr/testify/require"
)

func TestHostStatusOnlyInsideSessionHost(t *testing.T) {
	t.Parallel()
	m := newBusyUI(&countingWorkspace{ready: true})
	require.Nil(t, m.hostStatus())
}

func TestHostStatusIsSentOncePerChange(t *testing.T) {
	t.Parallel()
	m := newBusyUI(&countingWorkspace{ready: true})
	m.inSessionHost = true
	m.session.Title = "Remote setup"

	require.NotNil(t, m.hostStatus(), "the first status is always sent")
	require.Equal(t, sessionhost.Status{Title: "Remote setup", State: sessionhost.StateReady}, m.lastHostStatus)
	require.Nil(t, m.hostStatus(), "nothing changed")

	m.agentBusyCache.set(true)
	require.NotNil(t, m.hostStatus())
	require.Equal(t, sessionhost.StateWorking, m.lastHostStatus.State)

	m.dialog.OpenDialogWithGrace(dialog.NewPermissions(m.com, permission.PermissionRequest{ID: "p1", ToolName: "bash"}))
	require.NotNil(t, m.hostStatus())
	require.Equal(t, sessionhost.StateWaiting, m.lastHostStatus.State, "a prompt waiting for the user wins over working")
}

func TestHostStatusRidesOnUpdate(t *testing.T) {
	t.Parallel()
	m := newBusyUI(&countingWorkspace{ready: true})
	m.inSessionHost = true
	_, cmd := m.Update(tea.WindowSizeMsg{Width: 140, Height: 45})
	require.NotNil(t, cmd)
	require.True(t, m.hostStatusSent)
}
