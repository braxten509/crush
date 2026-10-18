package backend

import (
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/stretchr/testify/require"
)

// TestListHelpersScopedToChat: with one helper in each of two chats, each
// chat lists only its own, and a helper's child chat lists none
// (engine-design.md 5.3).
func TestListHelpersScopedToChat(t *testing.T) {
	r := newLiveRig(t)
	app := newClientID(t)
	ws := r.open(t, r.project(t), app)
	one, two := r.chat(t, ws, "one"), r.chat(t, ws, "two")
	taskOne, jobOne := r.spawnWork(t, ws, app, one, "one")
	taskTwo, jobTwo := r.spawnWork(t, ws, app, two, "two")

	for chat, want := range map[string]string{one: taskOne.ID, two: taskTwo.ID} {
		helpers, err := r.b.ListHelpers(ws.ID, chat)
		require.NoError(t, err)
		require.Len(t, helpers, 1, "chat %s lists only its own helper", chat)
		require.Equal(t, want, helpers[0].ID)
		require.Equal(t, chat, helpers[0].SessionID)
		require.Equal(t, "running", helpers[0].Status)
		require.Equal(t, app, helpers[0].StartedBy, "the app whose prompt started the turn owns the helper")
		require.NotEmpty(t, helpers[0].ChildSessionID)

		child, err := r.b.ListHelpers(ws.ID, helpers[0].ChildSessionID)
		require.NoError(t, err)
		require.Empty(t, child)
		require.NotNil(t, child, "an empty list is [], not null")
	}

	// The jobs are scoped the same way.
	for chat, want := range map[string]string{one: jobOne.JobID, two: jobTwo.JobID} {
		jobs, err := r.b.ListJobs(ws.ID, chat)
		require.NoError(t, err)
		require.Len(t, jobs, 1)
		require.Equal(t, want, jobs[0].JobID)
		require.Equal(t, chat, jobs[0].SessionID)
		require.Equal(t, app, jobs[0].StartedBy)
	}
	require.ErrorIs(t, r.b.KillJob(ws.ID, two, proto.JobRef{JobID: jobOne.JobID}), ErrJobNotFound, "another chat's job is not found")
	require.ErrorIs(t, r.b.StopHelper(ws.ID, two, taskOne.ID), ErrHelperNotFound)
	helpers, err := r.b.ListHelpers(ws.ID, one)
	require.NoError(t, err)
	require.Equal(t, "running", helpers[0].Status, "a refused stop leaves the helper running")
}

// TestMoveToBackgroundSignalsChatShell: Ctrl+B signals the running shells of
// the chat it names, and only those (engine-design.md 5.4).
func TestMoveToBackgroundSignalsChatShell(t *testing.T) {
	b, _ := newTestBackend(t)
	ws, _ := insertTestWorkspace(t, b, t.TempDir())
	chatA, chatB := "chat-a-"+newClientID(t), "chat-b-"+newClientID(t)
	signal := make(chan struct{}, 1)
	t.Cleanup(tools.RegisterShellBackground(chatA, "marker-"+newClientID(t), signal))

	moved, err := b.MoveToBackground(ws.ID, chatB)
	require.NoError(t, err)
	require.False(t, moved, "chat B has no running command")
	select {
	case <-signal:
		t.Fatal("chat A's shell was signalled for chat B")
	case <-time.After(50 * time.Millisecond):
	}

	moved, err = b.MoveToBackground(ws.ID, chatA)
	require.NoError(t, err)
	require.True(t, moved)
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal("chat A's shell was not signalled")
	}

	_, err = b.MoveToBackground("no-such-workspace", chatA)
	require.ErrorIs(t, err, ErrWorkspaceNotFound)
}
