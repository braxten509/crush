package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/shell"
)

func readSavedJobs(t *testing.T, path string) []savedJob {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	var saved []savedJob
	require.NoError(t, json.Unmarshal(data, &saved))
	return saved
}

// savingJobHub is an interactive hub whose agent replies to every prompt and
// passes it on.
func savingJobHub(t *testing.T) (*taskHub, chan SessionAgentCall, string) {
	t.Helper()
	calls := make(chan SessionAgentCall, 8)
	c := sessionRunTestCoordinator(t, func(ctx context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		calls <- call
		return fakeSessionReply(call, "received"), nil
	})
	c.interactive = true
	h := c.tasks
	h.dir = t.TempDir()
	return h, calls, filepath.Join(c.cfg.Config().Options.DataDirectory, savedJobsFile)
}

func startJob(t *testing.T, h *taskHub, sessionID, command string) *shell.BackgroundShell {
	t.Helper()
	response := h.background(TaskRequest{Session: sessionID, Background: &BackgroundRequest{Command: command, WorkingDir: t.TempDir(), Name: "author"}})
	require.Empty(t, response.Error)
	require.False(t, response.Background.IsError, response.Background.Content)
	var meta tools.BashResponseMetadata
	require.NoError(t, json.Unmarshal([]byte(response.Background.Metadata), &meta))
	job, ok := shell.GetBackgroundShellManager().Get(meta.ShellID)
	require.True(t, ok)
	t.Cleanup(func() {
		_ = shell.GetBackgroundShellManager().Kill(job.ID)
		_ = shell.GetBackgroundShellManager().Remove(job.ID)
	})
	return job
}

func TestFinishedBackgroundJobIsForgotten(t *testing.T) {
	h, calls, path := savingJobHub(t)
	session, err := h.c.sessions.Create(t.Context(), "jobs")
	require.NoError(t, err)
	job := startJob(t, h, session.ID, "sleep 1; echo done")
	saved := readSavedJobs(t, path)
	require.Len(t, saved, 1)
	require.Equal(t, job.ID, saved[0].ID)
	require.Equal(t, session.ID, saved[0].SessionID)
	require.Equal(t, os.Getpid(), saved[0].Owner)

	select {
	case call := <-calls:
		require.Contains(t, call.Prompt, "exit code 0")
	case <-time.After(8 * time.Second):
		t.Fatal("completion notification missing")
	}
	require.Eventually(t, func() bool { return len(readSavedJobs(t, path)) == 0 }, 2*time.Second, 20*time.Millisecond)
}

func TestShutdownJobIsReportedByTheNextCrush(t *testing.T) {
	h, calls, path := savingJobHub(t)
	session, err := h.c.sessions.Create(t.Context(), "jobs")
	require.NoError(t, err)
	job := startJob(t, h, session.ID, "echo halfway; sleep 30")
	require.Eventually(t, func() bool { out, _, _, _ := job.GetOutput(); return out != "" }, 2*time.Second, 20*time.Millisecond)

	// Crush quits: agents stop first, then background shells are killed.
	h.stopAll()
	require.NoError(t, shell.GetBackgroundShellManager().Kill(job.ID))
	require.Eventually(t, func() bool {
		saved := readSavedJobs(t, path)
		return len(saved) == 1 && !saved[0].Interrupted.IsZero()
	}, 2*time.Second, 20*time.Millisecond)
	require.Contains(t, readSavedJobs(t, path)[0].Output, "halfway")
	select {
	case call := <-calls:
		t.Fatalf("a closing Crush must not run the session: %q", call.Prompt)
	case <-time.After(200 * time.Millisecond):
	}

	// The next Crush finds the job of a Crush that is gone.
	dead := deadPID(t)
	require.NoError(t, h.editSavedJobs(func(saved []savedJob) []savedJob {
		saved[0].Owner = dead
		return saved
	}))
	h.reportInterruptedJobs(t.Context())
	select {
	case call := <-calls:
		require.Equal(t, session.ID, call.SessionID)
		name, status, ok := ParseTaskNotification(call.Prompt)
		require.True(t, ok)
		require.Equal(t, BackgroundProcessName, name)
		require.Equal(t, string(TaskStopped), status)
		require.Contains(t, call.Prompt, "Job "+job.ID+" (author)")
		require.Contains(t, call.Prompt, "sleep 30")
		require.Contains(t, call.Prompt, "halfway")
		require.Contains(t, call.Prompt, "Don't end your turn waiting")
	case <-time.After(5 * time.Second):
		t.Fatal("interrupted job was not reported")
	}
	require.Empty(t, readSavedJobs(t, path), "a reported job must not be reported again")
}

func TestClaimInterruptedJobsTakesOnlyJobsOfGoneCrushes(t *testing.T) {
	t.Parallel()
	h, _ := savingHub(t, true)
	dead := deadPID(t)
	now := time.Now()
	seed := []savedJob{
		{ID: "001", SessionID: "s", Owner: dead, Started: now, Interrupted: now},
		{ID: "002", SessionID: "s", Owner: dead, Started: now}, // crashed
		{ID: "003", SessionID: "s", Owner: os.Getpid(), Started: now},
		{ID: "004", SessionID: "s", Owner: dead, Started: now.Add(-2 * resumeWindow)},
	}
	require.NoError(t, h.editSavedJobs(func([]savedJob) []savedJob { return seed }))
	claimed, err := h.claimInterruptedJobs()
	require.NoError(t, err)
	var ids []string
	for _, s := range claimed {
		ids = append(ids, s.ID)
	}
	require.Equal(t, []string{"001", "002"}, ids)
	left := readSavedJobs(t, h.savedDataPath(savedJobsFile))
	require.Len(t, left, 1)
	require.Equal(t, "003", left[0].ID)

	notice := interruptedJobsNotification(claimed)
	require.Contains(t, notice, "ended when Crush closed")
	require.Contains(t, notice, "ended when Crush stopped unexpectedly")
}

func TestNonInteractiveRunsSaveNoJobs(t *testing.T) {
	t.Parallel()
	h, _ := savingHub(t, false)
	require.Empty(t, h.savedDataPath(savedJobsFile))
}
