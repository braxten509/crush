package agent

import (
	"context"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"github.com/stretchr/testify/require"

	"github.com/charmbracelet/crush/internal/config"
)

func TestStopSessionTaskTellsParent(t *testing.T) {
	notified := make(chan string, 4)
	c := sessionRunTestCoordinator(t, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		notified <- call.Prompt
		return fakeSessionReply(call, "received"), nil
	})
	h := resumeTestHub(t, c)
	parent, child := helperChats(t, c)
	m := Model{CatwalkCfg: catwalk.Model{DefaultMaxTokens: 4096}}
	agent := newMockAgent("test", 4096, func(ctx context.Context, _ SessionAgentCall) (*fantasy.AgentResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	released := make(chan struct{})
	snapshot, err := h.start(t.Context(), func() { close(released) }, &Task{SessionID: parent.ID, ChildID: child.ID, Name: "worker", Status: TaskRunning, Started: time.Now()}, agent, m, config.ProviderConfig{ID: "test"}, "work")
	require.NoError(t, err)

	require.ErrorIs(t, StopSessionTask("another-chat", snapshot.ID), ErrTaskNotInSession)
	h.mu.Lock()
	require.Equal(t, TaskRunning, h.tasks[snapshot.ID].Status, "a stop from another chat leaves it running")
	h.mu.Unlock()

	require.NoError(t, StopSessionTask(parent.ID, snapshot.ID))
	h.mu.Lock()
	require.True(t, h.userStopped[snapshot.ID], "a user stop, so the parent is told")
	h.mu.Unlock()
	select {
	case prompt := <-notified:
		name, status, ok := ParseTaskNotification(prompt)
		require.True(t, ok)
		require.Equal(t, "worker", name)
		require.Equal(t, string(TaskStopped), status)
		require.Contains(t, prompt, "The user stopped this task before it finished.")
	case <-time.After(5 * time.Second):
		t.Fatal("the parent was not told")
	}
	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Fatal("the stopped helper did not end")
	}
	require.NoError(t, StopSessionTask(parent.ID, snapshot.ID), "stopping an ended helper changes nothing")
	select {
	case prompt := <-notified:
		t.Fatalf("an ended helper's parent is told once: %q", prompt)
	case <-time.After(200 * time.Millisecond):
	}
}

// TestFinishUnknownTaskIsNoop: finishing a task the hub does not list does
// nothing, instead of reading a nil task.
func TestFinishUnknownTaskIsNoop(t *testing.T) {
	h := &taskHub{tasks: map[string]*Task{}}
	require.NotPanics(t, func() { h.finish(t.Context(), "t9", "output", nil) })
}
