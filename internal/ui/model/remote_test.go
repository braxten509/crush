package model

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/charmbracelet/crush/internal/remote"
	"github.com/charmbracelet/crush/internal/session"
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
