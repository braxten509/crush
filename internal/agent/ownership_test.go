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

func TestInterruptJobNoHandoff(t *testing.T) {
	h, calls, path := savingJobHub(t)
	listTestHub(t, h)
	session, err := h.c.sessions.Create(t.Context(), "jobs")
	require.NoError(t, err)
	hold, _ := gate(t)
	job := startJob(t, h, session.ID, "echo halfway; "+hold)
	require.Eventually(t, func() bool { out, _, _, _ := job.GetOutput(); return out != "" }, 2*time.Second, 20*time.Millisecond)

	require.NoError(t, InterruptJob(Process{JobID: job.ID}))
	require.True(t, job.IsDone())
	require.Eventually(t, func() bool {
		saved := readSavedJobs(t, path)
		return len(saved) == 1 && !saved[0].Interrupted.IsZero() && saved[0].Owner == 0
	}, 2*time.Second, 20*time.Millisecond, "the job is saved interrupted and released")
	require.Contains(t, readSavedJobs(t, path)[0].Output, "halfway")
	select {
	case call := <-calls:
		t.Fatalf("an interrupted job must not start a turn: %q", call.Prompt)
	case <-time.After(500 * time.Millisecond):
	}
	claimed, err := h.claimInterruptedJobs()
	require.NoError(t, err)
	require.Len(t, claimed, 1, "the next resume reports it, as after a restart")
	require.Equal(t, job.ID, claimed[0].ID)
}

func TestInterruptTaskResumesUnderItsID(t *testing.T) {
	c := sessionRunTestCoordinator(t, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		t.Errorf("the parent must not be told about an interrupted helper: %q", call.Prompt)
		return fakeSessionReply(call, "ok"), nil
	})
	c.interactive = true
	h := resumeTestHub(t, c)
	parent, child := helperChats(t, c)
	m := Model{CatwalkCfg: catwalk.Model{DefaultMaxTokens: 4096}}
	agent := newMockAgent("test", 4096, func(ctx context.Context, _ SessionAgentCall) (*fantasy.AgentResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	released := make(chan struct{})
	snapshot, err := h.start(t.Context(), func() { close(released) }, &Task{SessionID: parent.ID, ChildID: child.ID, Status: TaskRunning, Started: time.Now()}, agent, m, config.ProviderConfig{ID: "test"}, "work")
	require.NoError(t, err)

	require.NoError(t, InterruptTask(snapshot.ID))
	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Fatal("the interrupted helper did not end")
	}
	h.mu.Lock()
	ended := *h.tasks[snapshot.ID]
	_, marked := h.interrupting[snapshot.ID]
	h.mu.Unlock()
	require.Equal(t, TaskStopped, ended.Status)
	require.False(t, marked, "the mark goes with the run it ended")
	saved := readSaved(t, h.savedTasksPath())
	require.Len(t, saved, 1, "an interrupted helper stays saved")
	require.Zero(t, saved[0].Owner)
	require.False(t, saved[0].Interrupted.IsZero())
	require.NoError(t, InterruptTask(snapshot.ID), "an ended helper is a no-op")

	claimed, err := h.claimInterrupted()
	require.NoError(t, err)
	require.Len(t, claimed, 1, "this same process claims it again")
	h.mu.Lock()
	if other, taken := h.tasks[claimed[0].ID]; taken && other.ChildID != claimed[0].ChildID {
		t.Fatal("the resumed run would lose the helper's ID")
	}
	h.mu.Unlock()
}
