package model

import (
	"context"
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/charmbracelet/crush/internal/remote"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/dialog"
)

func TestRemoteActionsRunLikeKeys(t *testing.T) {
	t.Parallel()

	ws := &countingWorkspace{ready: true, queued: []string{"later"}}
	m := newBusyUI(ws)
	warmCaches(m, false)

	// A phone still showing another chat is refused.
	_, err := m.runRemoteAction(&remote.Action{Kind: remote.ActCancel, SessionID: "other"})
	require.Error(t, err)

	_, err = m.runRemoteAction(&remote.Action{Kind: remote.ActClearQueue, SessionID: "s1"})
	require.NoError(t, err)
	require.Equal(t, 1, ws.clearQueueCalls)

	// YOLO toggles in code mode, as Ctrl+Y does.
	_, err = m.runRemoteAction(&remote.Action{Kind: remote.ActYolo, SessionID: "s1"})
	require.NoError(t, err)
	require.True(t, ws.yolo)

	// Model changes wait for the turn to end, as in the TUI.
	warmCaches(m, true)
	_, err = m.runRemoteAction(&remote.Action{Kind: remote.ActModel, SessionID: "s1", Provider: "codex", Model: "gpt-6-astra"})
	require.ErrorIs(t, err, errAgentBusy)
	_, err = m.runRemoteAction(&remote.Action{Kind: remote.ActMode, SessionID: "s1"})
	require.ErrorIs(t, err, errAgentBusy)
}

func TestRemoteSetupCancelDiscardsLateChecks(t *testing.T) {
	t.Parallel()
	m := newBusyUI(&countingWorkspace{})
	m.dialog = dialog.NewOverlay(dialog.NewRemoteSetup(m.com))
	m.remoteGeneration = 1
	m.remoteStarting = true
	ctx, cancel := context.WithCancel(t.Context())
	m.remoteSetupCancel = cancel
	m.closeRemoteSetup()
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.False(t, m.remoteStarting)
	require.False(t, m.dialog.ContainsDialog(dialog.RemoteSetupID))
	err := &remote.SetupError{Info: remote.SetupInfo{State: remote.SetupMissing, Action: "install"}}
	require.Nil(t, m.handleRemoteStarted(remoteStartedMsg{generation: 1, err: err}))
	require.Nil(t, m.handleRemoteSetupPoll(remoteSetupPollMsg{generation: 1}))
	require.Nil(t, m.handleRemoteSetupFinished(remoteSetupFinishedMsg{generation: 1, err: errors.New("cancelled")}))
	require.False(t, m.dialog.ContainsDialog(dialog.RemoteSetupID), "late results must not reopen setup")
}

func TestRemoteSetupRequiresExplicitCurrentAction(t *testing.T) {
	t.Parallel()
	m := newBusyUI(&countingWorkspace{})
	m.dialog = dialog.NewOverlay(dialog.NewRemoteSetup(m.com))
	m.remoteSetupInfo = remote.SetupInfo{State: remote.SetupLogin, Action: "connect"}
	require.Nil(t, m.runRemoteSetup("install"))
	require.Nil(t, m.runRemoteSetup(""))
	m.remoteSetupBusy = true
	require.Nil(t, m.runRemoteSetup("connect"))
	m.remoteSetupBusy = false
	m.dialog.CloseDialog(dialog.RemoteSetupID)
	require.Nil(t, m.runRemoteSetup("connect"))
}

func TestRemoteSetupFailureStaysRetryable(t *testing.T) {
	t.Parallel()
	m := newBusyUI(&countingWorkspace{})
	m.dialog = dialog.NewOverlay(dialog.NewRemoteSetup(m.com))
	m.remoteGeneration = 4
	m.remoteStarting = true
	info := remote.SetupInfo{State: remote.SetupMissing, Title: "Install Tailscale", Action: "install", Button: "Install Tailscale"}
	cmd := m.handleRemoteStarted(remoteStartedMsg{generation: 4, err: &remote.SetupError{Info: info}})
	require.NotNil(t, cmd, "keep checking after manual installation elsewhere")
	require.False(t, m.remoteStarting)
	require.Equal(t, info, m.remoteSetupInfo)
	m.remoteSetupBusy = true
	require.Nil(t, m.handleRemoteSetupFinished(remoteSetupFinishedMsg{generation: 4, err: errors.New("cancelled")}))
	require.False(t, m.remoteSetupBusy)
	require.True(t, m.dialog.ContainsDialog(dialog.RemoteSetupID))
	d := m.dialog.Dialog(dialog.RemoteSetupID)
	require.Equal(t, dialog.ActionRemoteSetup{Action: "install"}, d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter}))
}

// newChatWorkspace creates the session a new chat's first message needs.
type newChatWorkspace struct {
	*countingWorkspace
	created int
}

func (w *newChatWorkspace) CreateSession(context.Context, string) (session.Session, error) {
	w.created++
	return session.Session{ID: "s2"}, nil
}

func TestRemoteActsOnTheNewChat(t *testing.T) {
	t.Parallel()

	ws := &newChatWorkspace{countingWorkspace: &countingWorkspace{ready: true}}
	m := newBusyUI(ws.countingWorkspace)
	m.com.Workspace = ws
	m.session = nil
	warmCaches(m, false)

	// A phone still showing the last chat is refused.
	_, err := m.runRemoteAction(&remote.Action{Kind: remote.ActCancel, SessionID: "s1"})
	require.Error(t, err)

	// Nothing is queued or worth summarizing before the first message.
	_, err = m.runRemoteAction(&remote.Action{Kind: remote.ActClearQueue})
	require.NoError(t, err)
	require.Zero(t, ws.clearQueueCalls)
	_, err = m.runRemoteAction(&remote.Action{Kind: remote.ActSummarize})
	require.Error(t, err)

	// The first message starts the chat, as typing it in the window does.
	_, err = m.runRemoteAction(&remote.Action{Kind: remote.ActSend, Text: "hello"})
	require.NoError(t, err)
	require.Equal(t, 1, ws.created)
	require.Equal(t, "s2", m.session.ID)
}

func TestRemoteBadge(t *testing.T) {
	t.Parallel()

	m := newBusyUI(&countingWorkspace{})
	require.NotContains(t, m.status.modeBadge(), "REMOTE")
	m.status.SetRemote(true)
	require.Contains(t, m.status.modeBadge(), "REMOTE")
}
