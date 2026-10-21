package model

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/ui/dialog/remotecontrol"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/remote"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/util"
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
	m.dialog = dialog.NewOverlay(remotecontrol.NewRemoteSetup(m.com))
	m.remote.generation = 1
	m.remote.starting = true
	ctx, cancel := context.WithCancel(t.Context())
	m.remote.setupCancel = cancel
	m.closeRemoteSetup()
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.False(t, m.remote.starting)
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
	m.dialog = dialog.NewOverlay(remotecontrol.NewRemoteSetup(m.com))
	m.remote.setupInfo = remote.SetupInfo{State: remote.SetupLogin, Action: "connect"}
	require.Nil(t, m.runRemoteSetup("install"))
	require.Nil(t, m.runRemoteSetup(""))
	m.remote.setupBusy = true
	require.Nil(t, m.runRemoteSetup("connect"))
	m.remote.setupBusy = false
	m.dialog.CloseDialog(dialog.RemoteSetupID)
	require.Nil(t, m.runRemoteSetup("connect"))
}

func TestRemoteSetupFailureStaysRetryable(t *testing.T) {
	t.Parallel()
	m := newBusyUI(&countingWorkspace{})
	m.dialog = dialog.NewOverlay(remotecontrol.NewRemoteSetup(m.com))
	m.remote.generation = 4
	m.remote.starting = true
	info := remote.SetupInfo{State: remote.SetupMissing, Title: "Install Tailscale", Action: "install", Button: "Install Tailscale"}
	cmd := m.handleRemoteStarted(remoteStartedMsg{generation: 4, err: &remote.SetupError{Info: info}})
	require.NotNil(t, cmd, "keep checking after manual installation elsewhere")
	require.False(t, m.remote.starting)
	require.Equal(t, info, m.remote.setupInfo)
	m.remote.setupBusy = true
	require.Nil(t, m.handleRemoteSetupFinished(remoteSetupFinishedMsg{generation: 4, err: errors.New("cancelled")}))
	require.False(t, m.remote.setupBusy)
	require.True(t, m.dialog.ContainsDialog(dialog.RemoteSetupID))
	d := m.dialog.Dialog(dialog.RemoteSetupID)
	require.Equal(t, remotecontrol.ActionRemoteSetup{Action: "install"}, d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter}))
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

// TestEngineRemoteFollowsTheShownChat: on the shared engine, REMOTE shows
// while the chat the window shows is shared, whichever app turned it on;
// switching chats follows each chat's share; a share list read before a
// change does not undo it.
func TestEngineRemoteFollowsTheShownChat(t *testing.T) {
	t.Parallel()
	m := newBusyUI(&countingWorkspace{})
	on := func(chat string, phones ...string) proto.RemoteStatus {
		return proto.RemoteStatus{State: "on", SessionID: chat, Phones: phones}
	}
	shows := func() bool { return strings.Contains(m.status.modeBadge(), "REMOTE") }

	require.Nil(t, m.handleRemoteEngineStatus(on("s2")))
	require.False(t, shows(), "another chat's share")
	require.Nil(t, m.handleRemoteEngineStatus(on("s1")))
	require.True(t, shows())
	require.True(t, m.remoteActive())
	cmd := m.handleRemoteEngineStatus(on("s1", "Pixel"))
	require.NotNil(t, cmd)
	require.Equal(t, "Remote Control: Pixel connected", cmd().(util.InfoMsg).Msg)

	m.session = &session.Session{ID: "s2"}
	m.syncRemotePresence()
	require.True(t, shows(), "s2 is shared too")
	m.session = &session.Session{ID: "s3"}
	m.syncRemotePresence()
	require.False(t, shows())
	m.session = &session.Session{ID: "s1"}
	m.syncRemotePresence()

	// Turned off in the other app.
	require.Nil(t, m.handleRemoteEngineStatus(proto.RemoteStatus{State: "off", SessionID: "s1"}))
	require.False(t, shows())
	require.False(t, m.remoteActive())

	since := m.remote.changes
	m.handleRemoteEngineStatus(on("s1"))
	m.handleRemoteEngineStatus(proto.RemoteStatus{State: "off", SessionID: "s2"})
	m.handleEngineShares(remoteEngineSharesMsg{shares: []proto.RemoteStatus{on("s2"), on("s4")}, changes: since})
	require.True(t, shows(), "s1 started after the list was asked for")
	require.NotContains(t, m.remote.engine, "s2", "s2 ended after the list was asked for")
	require.Contains(t, m.remote.engine, "s4")
	m.handleEngineShares(remoteEngineSharesMsg{shares: []proto.RemoteStatus{on("s4")}, changes: m.remote.changes})
	require.False(t, shows(), "a newer list")
}

// The list read at start-up can come before any change: a change heard
// after it must not crash the window (turning Remote off from the dialog
// did, 2026-10-21).
func TestEngineRemoteChangeAfterTheFirstList(t *testing.T) {
	t.Parallel()
	m := newBusyUI(&countingWorkspace{})
	m.handleEngineShares(remoteEngineSharesMsg{shares: []proto.RemoteStatus{{State: "on", SessionID: "s1"}}})
	require.True(t, m.engineShared())
	require.NotPanics(t, func() {
		m.handleRemoteEngineStatus(proto.RemoteStatus{State: "off", SessionID: "s1"})
	})
	require.False(t, m.engineShared())
}
