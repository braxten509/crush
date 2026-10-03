package agent

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
)

func savingHub(t *testing.T, interactive bool) (*taskHub, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.NewTestStore(&config.Config{
		Options:   &config.Options{DataDirectory: dir},
		Providers: csync.NewMap[string, config.ProviderConfig](),
	})
	h := &taskHub{
		c:     &coordinator{cfg: cfg, interactive: interactive},
		tasks: map[string]*Task{}, cancels: map[string]context.CancelFunc{}, userStopped: map[string]bool{},
	}
	return h, filepath.Join(dir, savedTasksFile)
}

func readSaved(t *testing.T, path string) []savedTask {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	var saved []savedTask
	require.NoError(t, json.Unmarshal(data, &saved))
	return saved
}

// deadPID returns the ID of a process that has exited.
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	require.NoError(t, cmd.Run())
	return cmd.Process.Pid
}

func TestSavedTasksSurviveShutdownButNotTheirEnd(t *testing.T) {
	t.Parallel()
	h, path := savingHub(t, true)
	running := Task{ID: "t1", SessionID: "parent", ChildID: "child-1", Status: TaskRunning, Started: time.Now()}
	ended := Task{ID: "t2", SessionID: "parent", ChildID: "child-2", Status: TaskRunning, Started: time.Now()}
	h.remember(running, "codex")
	h.remember(ended, "codex")
	h.forget(ended.ChildID)

	h.tasks["t1"] = &running
	h.cancels["t1"] = func() {}
	h.dir = t.TempDir()
	h.stopAll()

	saved := readSaved(t, path)
	require.Len(t, saved, 1)
	require.Equal(t, "child-1", saved[0].ChildID)
	require.Equal(t, "codex", saved[0].Provider)
	require.Equal(t, os.Getpid(), saved[0].Owner)
	require.False(t, saved[0].Interrupted.IsZero(), "shutdown must record when it cut the task off")
}

func TestClaimInterruptedTakesOnlyTasksOfGoneCrushes(t *testing.T) {
	t.Parallel()
	h, path := savingHub(t, true)
	dead := deadPID(t)
	now := time.Now()
	seed := []savedTask{
		{Task: Task{ID: "t1", ChildID: "gone", Started: now}, Owner: dead, Interrupted: now},
		{Task: Task{ID: "t2", ChildID: "crashed", Started: now}, Owner: dead},
		{Task: Task{ID: "t3", ChildID: "other-crush", Started: now}, Owner: os.Getppid()},
		{Task: Task{ID: "t4", ChildID: "mine", Started: now}, Owner: os.Getpid()},
		{Task: Task{ID: "t5", ChildID: "stale", Started: now.Add(-3 * resumeWindow)}, Owner: dead, Interrupted: now.Add(-2 * resumeWindow)},
	}
	data, err := json.Marshal(seed)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o600))

	claimed, err := h.claimInterrupted()
	require.NoError(t, err)
	var ids []string
	for _, s := range claimed {
		ids = append(ids, s.ChildID)
		require.Equal(t, os.Getpid(), s.Owner)
	}
	require.ElementsMatch(t, []string{"gone", "crashed"}, ids)

	var left []string
	for _, s := range readSaved(t, path) {
		left = append(left, s.ChildID)
	}
	require.ElementsMatch(t, []string{"gone", "crashed", "other-crush", "mine"}, left, "stale tasks are dropped; claimed ones stay saved under this Crush")

	again, err := h.claimInterrupted()
	require.NoError(t, err)
	require.Empty(t, again, "a task is claimed once")
}

func TestNonInteractiveRunsSaveNoTasks(t *testing.T) {
	t.Parallel()
	h, path := savingHub(t, false)
	h.remember(Task{ID: "t1", ChildID: "child"}, "codex")
	require.NoFileExists(t, path)
}

func TestTaskNumber(t *testing.T) {
	t.Parallel()
	n, ok := taskNumber("t32")
	require.True(t, ok)
	require.Equal(t, 32, n)
	_, ok = taskNumber("x")
	require.False(t, ok)
}
