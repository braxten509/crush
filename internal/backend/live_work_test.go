package backend

import (
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
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

// TestEndedWorkLeavesOwners: an app's helper and jobs are forgotten once
// they end the usual way: the helper at its helper event, a job once it is
// gone from the job list, read by the next job start or the next jobs list.
// An ended event about a helper that runs again under its task ID, as a
// resumed one does, keeps its owner (engine-design.md 5.5).
func TestEndedWorkLeavesOwners(t *testing.T) {
	r := newLiveRig(t)
	app := newClientID(t)
	ws := r.open(t, r.project(t), app)
	c, d, e := r.chat(t, ws, "C"), r.chat(t, ws, "D"), r.chat(t, ws, "E")
	helper, first := r.spawnWork(t, ws, app, c, "c1")
	helperKey := workKey{kind: agent.WorkHelper, id: helper.ID}
	owner := func(key workKey) (string, bool) {
		ws.live.mu.Lock()
		defer ws.live.mu.Unlock()
		o, ok := ws.live.owners[key]
		return o, ok
	}
	known := func(key workKey) bool { _, ok := owner(key); return ok }
	for _, key := range []workKey{helperKey, jobKey(first)} {
		o, ok := owner(key)
		require.True(t, ok)
		require.Equal(t, app, o)
	}

	ws.SendEvent(pubsub.Event[agent.Task]{Type: pubsub.UpdatedEvent, Payload: agent.Task{ID: helper.ID, SessionID: c, Status: agent.TaskDone}})
	time.Sleep(300 * time.Millisecond)
	require.True(t, known(helperKey), "an ended event about a helper that runs keeps its owner")

	require.NoError(t, agent.StopSessionTask(c, helper.ID))
	require.Eventually(t, func() bool { return !known(helperKey) }, 10*time.Second, 10*time.Millisecond,
		"an ended helper is forgotten at its event")

	require.NoError(t, r.b.KillJob(ws.ID, c, proto.JobRef{JobID: first.JobID}))
	require.Eventually(t, func() bool { return len(agent.SessionBackgroundProcesses(c)) == 0 }, 20*time.Second, 20*time.Millisecond)
	_, second := r.spawnWork(t, ws, app, d, "d1")
	require.Eventually(t, func() bool { return !known(jobKey(first)) && known(jobKey(second)) }, 10*time.Second, 10*time.Millisecond,
		"the next job start forgets an ended job, and records its own")

	require.NoError(t, r.b.KillJob(ws.ID, d, proto.JobRef{JobID: second.JobID}))
	require.Eventually(t, func() bool {
		jobs, err := r.b.ListJobs(ws.ID, e)
		require.NoError(t, err)
		return len(jobs) == 0 && len(agent.SessionBackgroundProcesses(d)) == 0 && !known(jobKey(second))
	}, 20*time.Second, 20*time.Millisecond, "a jobs list, of any chat, forgets an ended job")
}
