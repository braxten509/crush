package model

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/charmbracelet/crush/internal/remote"
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

func TestRemoteBadge(t *testing.T) {
	t.Parallel()

	m := newBusyUI(&countingWorkspace{})
	require.NotContains(t, m.status.modeBadge(), "REMOTE")
	m.status.SetRemote(true)
	require.Contains(t, m.status.modeBadge(), "REMOTE")
}
