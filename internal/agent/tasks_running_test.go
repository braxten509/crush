package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/shell"
	"github.com/stretchr/testify/require"
)

func TestSessionHasUnfinishedWork(t *testing.T) {
	h := registerTestHub(t)
	sessionID := t.Name()
	h.tasks = map[string]*Task{
		"helper": {SessionID: sessionID, Status: TaskRunning},
	}
	require.True(t, SessionHasUnfinishedWork(sessionID))
	require.False(t, SessionHasUnfinishedWork("other"))
	require.False(t, SessionHasUnfinishedWork(""))

	for _, status := range []TaskStatus{TaskDone, TaskFailed, TaskStopped} {
		h.tasks["helper"].Status = status
		h.tasks["helper"].Delivered = false
		h.tasks["helper"].delivering = true
		require.True(t, SessionHasUnfinishedWork(sessionID), "the result still needs to reach its parent")
		require.False(t, h.hasRunning(sessionID), "result delivery must not suppress the parent's final ding")
		h.tasks["helper"].Delivered = true
		h.tasks["helper"].delivering = false
		require.False(t, SessionHasUnfinishedWork(sessionID), "delivered results do not hold the UI waiting")
		h.tasks["helper"].Delivered = false
		require.False(t, SessionHasUnfinishedWork(sessionID), "failed delivery must not leave the UI waiting forever")
	}

	h.backgroundShells = map[string]*shell.BackgroundShell{"call": {ID: "job"}}
	h.backgroundOwners = map[string]string{"job": sessionID}
	require.True(t, SessionHasUnfinishedWork(sessionID), "a managed job does not need a process-table entry")
	require.False(t, SessionHasUnfinishedWork("other"))

	h.backgroundServices = map[string]bool{"job": true}
	require.False(t, SessionHasUnfinishedWork(sessionID), "an ongoing service permits completion")
	h.tasks["helper"].Status = TaskRunning
	require.True(t, SessionHasUnfinishedWork(sessionID), "a service does not hide an unfinished helper")

	delete(h.tasks, "helper")
	delete(h.backgroundServices, "job")
	delete(h.backgroundShells, "call")
	require.False(t, SessionHasUnfinishedWork(sessionID))
}

type pausedTaskPublisher struct {
	pubsub.Publisher[Task]
	ctx       context.Context
	published chan Task
	deliver   <-chan struct{}
}

func (p pausedTaskPublisher) Publish(_ pubsub.EventType, task Task) {
	p.published <- task
	select {
	case <-p.deliver:
	case <-p.ctx.Done():
	}
}

func TestSessionStaysUnfinishedDuringTaskResultHandoff(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "success"},
		{name: "failed", err: errors.New("parent provider failed")},
		{name: "cancelled", err: context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			testTaskResultHandoff(t, test.err)
		})
	}
}

func testTaskResultHandoff(t *testing.T, deliveryError error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	h := registerTestHub(t)
	parentCalled := make(chan struct{}, 1)
	c := sessionRunTestCoordinator(t, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		parentCalled <- struct{}{}
		require.True(t, SessionHasUnfinishedWork(call.SessionID))
		require.False(t, h.hasRunning(call.SessionID), "the parent's answer may still produce its completion notification")
		if deliveryError != nil {
			return nil, deliveryError
		}
		return fakeSessionReply(call, "reviewed result"), nil
	})
	c.interactive, c.tasks, h.c = true, h, c
	parent, err := c.sessions.Create(ctx, "parent")
	require.NoError(t, err)
	child, err := c.sessions.Create(ctx, "child")
	require.NoError(t, err)
	h.tasks = map[string]*Task{"helper": {ID: "helper", SessionID: parent.ID, ChildID: child.ID, Status: TaskRunning}}
	published, deliver := make(chan Task, 1), make(chan struct{})
	h.events = pausedTaskPublisher{ctx: ctx, published: published, deliver: deliver}
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.finish(ctx, "helper", "done", nil)
	}()
	select {
	case task := <-published:
		require.Equal(t, TaskDone, task.Status)
	case <-ctx.Done():
		t.Fatal("helper did not finish")
	}
	require.True(t, SessionHasUnfinishedWork(parent.ID), "the gray state must survive the gap before parent delivery")
	require.Empty(t, parentCalled)
	close(deliver)
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("helper result was not delivered")
	}
	require.Len(t, parentCalled, 1)
	require.False(t, SessionHasUnfinishedWork(parent.ID))
	require.Equal(t, deliveryError == nil, h.tasks["helper"].Delivered, "delivery success remains separate from pending work")
}

func TestSessionStaysUnfinishedAfterBackgroundJobExits(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	h := registerTestHub(t)
	manager := shell.GetBackgroundShellManager()
	job, err := manager.Start(ctx, t.TempDir(), nil, "true", "finished job")
	require.NoError(t, err)
	t.Cleanup(func() { _ = manager.Remove(job.ID) })
	require.True(t, job.WaitContext(ctx))
	h.backgroundShells = map[string]*shell.BackgroundShell{"call": job}
	h.backgroundOwners = map[string]string{job.ID: t.Name()}
	require.True(t, SessionHasUnfinishedWork(t.Name()), "an exited job still has a result to deliver")
	require.False(t, h.hasRunning(t.Name()), "delivery does not block the parent's final ding")
	delete(h.backgroundShells, "call")
	require.False(t, SessionHasUnfinishedWork(t.Name()))
}
