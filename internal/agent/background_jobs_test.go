package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/hooks"
	"github.com/charmbracelet/crush/internal/shell"
	"github.com/stretchr/testify/require"
)

func TestBackgroundJobRoundTripAndCompletion(t *testing.T) {
	notices := make(chan string, 4)
	c := sessionRunTestCoordinator(t, func(ctx context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		notices <- call.Prompt
		return fakeSessionReply(call, "received"), nil
	})
	session, err := c.sessions.Create(t.Context(), "background test")
	require.NoError(t, err)
	h := c.tasks
	h.dir = t.TempDir()
	hubsMu.Lock()
	hubs = append(hubs, h)
	hubsMu.Unlock()
	t.Cleanup(func() {
		hubsMu.Lock()
		for i, hub := range hubs {
			if hub == h {
				hubs = append(hubs[:i], hubs[i+1:]...)
				break
			}
		}
		hubsMu.Unlock()
	})
	response := h.background(TaskRequest{Session: session.ID, Background: &BackgroundRequest{Command: "sleep 3; printf finished; exit 7", WorkingDir: t.TempDir(), Name: "example"}})
	require.Empty(t, response.Error)
	require.NotNil(t, response.Background)
	require.False(t, response.Background.IsError, response.Background.Content)
	var meta tools.BashResponseMetadata
	require.NoError(t, json.Unmarshal([]byte(response.Background.Metadata), &meta))
	require.NotEmpty(t, meta.ShellID)
	job, ok := shell.GetBackgroundShellManager().Get(meta.ShellID)
	require.True(t, ok)
	t.Cleanup(func() {
		if !job.IsDone() {
			_ = shell.GetBackgroundShellManager().Kill(job.ID)
		}
		_ = shell.GetBackgroundShellManager().Remove(job.ID)
	})
	require.Eventually(t, func() bool { return len(BackgroundProcesses()) > 0 }, time.Second, 20*time.Millisecond)
	output := h.background(TaskRequest{Session: session.ID, Background: &BackgroundRequest{OutputID: job.ID}})
	require.Empty(t, output.Error)
	require.Contains(t, output.Background.Content, "Status: running")
	stranger, err := c.sessions.Create(t.Context(), "other")
	require.NoError(t, err)
	denied := h.background(TaskRequest{Session: stranger.ID, Background: &BackgroundRequest{StopID: job.ID}})
	require.Contains(t, denied.Error, "no background job")
	select {
	case notice := <-notices:
		require.Contains(t, notice, "<status>failed</status>")
		require.Contains(t, notice, "exit code 7")
		require.Contains(t, notice, "finished")
	case <-time.After(8 * time.Second):
		t.Fatal("completion notification missing")
	}
	output = h.background(TaskRequest{Session: session.ID, Background: &BackgroundRequest{OutputID: job.ID}})
	require.Empty(t, output.Error)
	require.NotNil(t, output.Background)
	require.Contains(t, output.Background.Content, "finished")
}

func TestBackgroundOutputAfterCompletedSessionRun(t *testing.T) {
	c := sessionRunTestCoordinator(t, func(ctx context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		return fakeSessionReply(call, "received"), nil
	})
	session, err := c.sessions.Create(t.Context(), "completed run")
	require.NoError(t, err)
	manager := shell.GetBackgroundShellManager()
	job, err := manager.Start(t.Context(), t.TempDir(), nil, "printf finished", "completed job")
	require.NoError(t, err)
	t.Cleanup(func() { _ = manager.Remove(job.ID) })
	require.True(t, job.WaitContext(t.Context()))
	h := c.tasks
	h.backgroundOwners = map[string]string{job.ID: session.ID}
	// Reproduce the interval between the run completing and its removal from
	// the hub: a new output request must not inherit the expired run context.
	run := newSessionRun(t.Context(), session.ID)
	run.release(nil)
	run.cancel()
	h.sessionRuns = map[string]*sessionRun{session.ID: run}
	output := h.background(TaskRequest{Session: session.ID, Background: &BackgroundRequest{OutputID: job.ID}})
	require.Empty(t, output.Error)
	require.NotNil(t, output.Background)
	require.Contains(t, output.Background.Content, "finished")
}

func TestBackgroundJobHonorsHooksAndRejectsInvalidRequests(t *testing.T) {
	c := newGateTestCoordinator(t, true)
	require.NoError(t, c.readyWg.Wait())
	h := &taskHub{c: c, dir: t.TempDir()}
	session, err := c.sessions.Create(t.Context(), "guarded background")
	require.NoError(t, err)
	marker := filepath.Join(t.TempDir(), "should-not-exist")
	c.cfg.Config().Hooks = map[string][]config.HookConfig{hooks.EventPreToolUse: {{Command: `echo '{"decision":"deny","reason":"test guard"}'`}}}
	require.NoError(t, c.cfg.Config().ValidateHooks())
	response := h.background(TaskRequest{Session: session.ID, Background: &BackgroundRequest{Command: "touch " + marker}})
	require.Empty(t, response.Error)
	require.True(t, response.Background.IsError)
	require.Contains(t, response.Background.Content, "test guard")
	_, err = os.Stat(marker)
	require.ErrorIs(t, err, os.ErrNotExist)
	response = h.background(TaskRequest{Session: session.ID, Background: &BackgroundRequest{Command: "echo hi", OutputID: "001"}})
	require.Contains(t, response.Error, "exactly one")
}

func TestBackgroundJobStopFromProcessListCancelsWholeScript(t *testing.T) {
	notices := make(chan string, 2)
	c := sessionRunTestCoordinator(t, func(ctx context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		notices <- call.Prompt
		return fakeSessionReply(call, "received"), nil
	})
	session, err := c.sessions.Create(t.Context(), "stop background test")
	require.NoError(t, err)
	h := c.tasks
	h.dir = t.TempDir()
	hubsMu.Lock()
	hubs = append(hubs, h)
	hubsMu.Unlock()
	t.Cleanup(func() {
		hubsMu.Lock()
		for i, hub := range hubs {
			if hub == h {
				hubs = append(hubs[:i], hubs[i+1:]...)
				break
			}
		}
		hubsMu.Unlock()
	})
	directory := t.TempDir()
	response := h.background(TaskRequest{Session: session.ID, Background: &BackgroundRequest{Command: "sleep 30; printf wrong > should-not-exist", WorkingDir: directory}})
	require.Empty(t, response.Error)
	require.NotNil(t, response.Background)
	var metadata tools.BashResponseMetadata
	require.NoError(t, json.Unmarshal([]byte(response.Background.Metadata), &metadata))
	job, ok := shell.GetBackgroundShellManager().Get(metadata.ShellID)
	require.True(t, ok)
	t.Cleanup(func() {
		if !job.IsDone() {
			_ = shell.GetBackgroundShellManager().Kill(job.ID)
		}
	})
	var process Process
	require.Eventually(t, func() bool {
		for _, p := range BackgroundProcesses() {
			if p.Command == job.Command {
				process = p
				return true
			}
		}
		return false
	}, time.Second, 20*time.Millisecond)
	require.Equal(t, job.ID, process.JobID)
	require.NoError(t, StopBackgroundProcess(process))
	require.True(t, job.IsDone())
	_, err = os.Stat(filepath.Join(directory, "should-not-exist"))
	require.ErrorIs(t, err, os.ErrNotExist)
	select {
	case notice := <-notices:
		require.Contains(t, notice, "<status>failed</status>")
	case <-time.After(5 * time.Second):
		t.Fatal("stopped job did not report completion")
	}
}

func TestBackgroundJobPersistsLateFileReview(t *testing.T) {
	notices := make(chan string, 1)
	c := sessionRunTestCoordinator(t, func(ctx context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		notices <- call.Prompt
		return fakeSessionReply(call, "received"), nil
	})
	session, err := c.sessions.Create(t.Context(), "review background test")
	require.NoError(t, err)
	h := c.tasks
	h.dir = t.TempDir()
	directory, err := filepath.EvalSymlinks(visibleReviewRoot(t))
	require.NoError(t, err)
	response := h.background(TaskRequest{Session: session.ID, Background: &BackgroundRequest{Command: "sleep 2; printf changed > output.txt", WorkingDir: directory}})
	require.Empty(t, response.Error)
	require.NotNil(t, response.Background)
	var metadata tools.BashResponseMetadata
	require.NoError(t, json.Unmarshal([]byte(response.Background.Metadata), &metadata))
	t.Cleanup(func() { _ = shell.GetBackgroundShellManager().Remove(metadata.ShellID) })
	select {
	case <-notices:
	case <-time.After(8 * time.Second):
		t.Fatal("missing completion")
	}
	require.Eventually(t, func() bool {
		messages, err := c.messages.List(t.Context(), session.ID)
		if err != nil {
			return false
		}
		for _, msg := range messages {
			full, err := c.messages.LoadReview(t.Context(), msg.ID)
			if err != nil {
				continue
			}
			for _, result := range full.ToolResults() {
				if result.Name == tools.BashToolName && result.Review != nil && len(result.Review.Changes) == 1 {
					return result.Review.Changes[0].Path == filepath.Join(directory, "output.txt")
				}
			}
		}
		return false
	}, 3*time.Second, 20*time.Millisecond)
}

func TestManagedJobsAreVisibleWithoutMarkedChildProcesses(t *testing.T) {
	h := registerTestHub(t)
	job, err := shell.GetBackgroundShellManager().Start(t.Context(), t.TempDir(), nil, "sleep 30", "logical job")
	require.NoError(t, err)
	t.Cleanup(func() { _ = shell.GetBackgroundShellManager().Kill(job.ID) })
	h.mu.Lock()
	h.backgroundShells = map[string]*shell.BackgroundShell{"logical": job}
	h.mu.Unlock()
	require.Empty(t, markedProcs([]string{TasksDirEnv + "=" + h.dir}))
	listed := BackgroundProcesses()
	found := false
	for _, process := range listed {
		if process.JobID == job.ID {
			found = true
			require.Equal(t, "sleep 30", process.Command)
		}
	}
	require.True(t, found, "managed jobs must not depend on process-table discovery")
}

func TestBackgroundServiceRemainsTrackedWithoutBlockingCompletion(t *testing.T) {
	h, _, savedPath := savingJobHub(t)
	sess, err := h.c.sessions.Create(t.Context(), "service")
	require.NoError(t, err)
	response := h.background(TaskRequest{Session: sess.ID, Background: &BackgroundRequest{
		Command: "sleep 30", WorkingDir: t.TempDir(), Name: "service", Service: true,
	}})
	require.Empty(t, response.Error)
	require.NotNil(t, response.Background)
	require.False(t, response.Background.IsError)
	var meta tools.BashResponseMetadata
	require.NoError(t, json.Unmarshal([]byte(response.Background.Metadata), &meta))
	job, ok := shell.GetBackgroundShellManager().Get(meta.ShellID)
	require.True(t, ok)
	t.Cleanup(func() {
		_ = shell.GetBackgroundShellManager().Kill(job.ID)
		_ = shell.GetBackgroundShellManager().Remove(job.ID)
	})
	require.False(t, job.IsDone())
	require.False(t, h.hasRunning(sess.ID), "services must not hold back a finish ding")
	saved := readSavedJobs(t, savedPath)
	require.Len(t, saved, 1)
	require.True(t, saved[0].Service)
	require.Contains(t, interruptedJobsNotification(saved), "crush bg --service")
	output := h.background(TaskRequest{Session: sess.ID, Background: &BackgroundRequest{OutputID: job.ID}})
	require.Empty(t, output.Error)
	require.Contains(t, output.Background.Content, "Status: running")
	stop := h.background(TaskRequest{Session: sess.ID, Background: &BackgroundRequest{StopID: job.ID}})
	require.Empty(t, stop.Error)
	require.False(t, stop.Background.IsError)
	require.True(t, job.WaitContext(t.Context()))
}
