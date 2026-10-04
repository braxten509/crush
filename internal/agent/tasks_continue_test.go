package agent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTaskIDsOutliveCrushForFollowUps(t *testing.T) {
	t.Parallel()
	h, _ := savingHub(t, true)
	done := Task{ID: "t7", SessionID: "parent", ChildID: "child-7", Name: "Author", CLI: "claude", Model: "opus", Status: TaskRunning, Started: time.Now()}
	h.remember(done, "claude")
	h.forget(done.ChildID)

	// A later Crush in the same project starts with no tasks in memory.
	later := &taskHub{c: h.c, tasks: map[string]*Task{}, cancels: map[string]context.CancelFunc{}, userStopped: map[string]bool{}}
	found, ok := later.findTask("parent", "t7")
	require.True(t, ok)
	require.Equal(t, "child-7", found.ChildID)
	_, ok = later.findTask("other", "t7")
	require.False(t, ok, "tasks belong to their own session")

	later.skipUsedIDs("parent")
	require.Equal(t, 7, later.seq, "new tasks must not reuse an earlier task's ID")
}

func TestContinueRefusesRunningAndUnknownTasks(t *testing.T) {
	t.Parallel()
	h, _ := savingHub(t, true)
	h.tasks["t1"] = &Task{ID: "t1", SessionID: "parent", ChildID: "child-1", Status: TaskRunning}
	_, err := h.continueTask(TaskRequest{Session: "parent", Continue: "t1", Prompt: "more"})
	require.ErrorContains(t, err, "still running")
	_, err = h.continueTask(TaskRequest{Session: "parent", Continue: "t9", Prompt: "more"})
	require.ErrorContains(t, err, "no task")
	_, err = h.continueTask(TaskRequest{Session: "parent", Continue: "t1", Prompt: " "})
	require.ErrorContains(t, err, "empty")
}
