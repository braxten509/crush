package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/agent/tools/shelltool"
	"github.com/charmbracelet/crush/internal/shell"
	"github.com/stretchr/testify/require"
)

// nativeJobTest is a hub listed in hubs, as a running Crush's is, whose
// agent turns are recorded in notices.
func nativeJobTest(t *testing.T) (*taskHub, string, chan string) {
	t.Helper()
	notices := make(chan string, 8)
	c := sessionRunTestCoordinator(t, func(ctx context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		notices <- call.Prompt
		return fakeSessionReply(call, "received"), nil
	})
	session, err := c.sessions.Create(t.Context(), "native background test")
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
	return h, session.ID, notices
}

// runNativeTool calls a tool the way a native agent's turn does, with the
// shell environment Crush gives that turn.
func runNativeTool(t *testing.T, env []string, sessionID string, tool fantasy.AgentTool, input any) fantasy.ToolResponse {
	t.Helper()
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, sessionID)
	ctx = context.WithValue(ctx, tools.ShellEnvContextKey, env)
	data, err := json.Marshal(input)
	require.NoError(t, err)
	response, err := tool.Run(ctx, fantasy.ToolCall{ID: "call-" + t.Name(), Name: tool.Info().Name, Input: string(data)})
	require.NoError(t, err)
	require.False(t, response.IsError, response.Content)
	return response
}

func startNativeJob(t *testing.T, h *taskHub, sessionID, command string) *shell.BackgroundShell {
	t.Helper()
	job, response := startJobWithEnv(t, h, h.env(sessionID), sessionID, command)
	require.Contains(t, response.Content, "Crush will report when it ends")
	return job
}

func startJobWithEnv(t *testing.T, h *taskHub, env []string, sessionID, command string) (*shell.BackgroundShell, fantasy.ToolResponse) {
	t.Helper()
	options := h.c.cfg.Config().Options
	bash := shelltool.NewBashTool(h.c.permissions, h.c.cfg.WorkingDir(), options.DataDirectory, options.Attribution, "", reportStartedJob)
	response := runNativeTool(t, env, sessionID, bash, tools.BashParams{Command: command, WorkingDir: t.TempDir(), RunInBackground: true})
	var meta tools.BashResponseMetadata
	require.NoError(t, json.Unmarshal([]byte(response.Metadata), &meta))
	job, ok := shell.GetBackgroundShellManager().Get(meta.ShellID)
	require.True(t, ok)
	t.Cleanup(func() {
		if !job.IsDone() {
			_ = shell.GetBackgroundShellManager().Kill(job.ID)
		}
		_ = shell.GetBackgroundShellManager().Remove(job.ID)
	})
	return job, response
}

// A job the agent's own bash call put in the background reports back once
// it ends, after the agent's turn is over.
func TestNativeBackgroundJobReportsOnceWhenItEnds(t *testing.T) {
	h, sessionID, notices := nativeJobTest(t)
	job := startNativeJob(t, h, sessionID, "sleep 2; printf finished")
	require.True(t, h.hasRunning(sessionID), "a running job holds the finished alert")
	select {
	case notice := <-notices:
		require.Contains(t, notice, "<name>Background process</name>")
		require.Contains(t, notice, "Job "+job.ID+" ")
		require.Contains(t, notice, "<status>completed</status>")
		require.Contains(t, notice, "finished")
	case <-time.After(8 * time.Second):
		t.Fatal("completion notification missing")
	}
	require.Never(t, func() bool { return len(notices) > 0 }, time.Second, 50*time.Millisecond, "the job was reported twice")
	require.Eventually(t, func() bool { return !h.hasRunning(sessionID) }, 2*time.Second, 20*time.Millisecond)
}

// The job shows once in the background list, as a job, and stopping it
// there tells the agent.
func TestNativeBackgroundJobListedOnceAndUserStopIsReported(t *testing.T) {
	h, sessionID, notices := nativeJobTest(t)
	job := startNativeJob(t, h, sessionID, "sleep 30; printf never")
	var listed []Process
	require.Eventually(t, func() bool {
		listed = nil
		for _, p := range BackgroundProcesses() {
			if p.JobID == job.ID || strings.Contains(p.Command, "sleep 30") {
				listed = append(listed, p)
			}
		}
		return len(listed) > 0
	}, time.Second, 20*time.Millisecond)
	require.Never(t, func() bool {
		count := 0
		for _, p := range BackgroundProcesses() {
			if p.JobID == job.ID || strings.Contains(p.Command, "sleep 30") {
				count++
			}
		}
		return count != 1
	}, 500*time.Millisecond, 50*time.Millisecond, "the job must be listed exactly once")
	require.Equal(t, job.ID, listed[0].JobID)
	require.NoError(t, StopBackgroundProcess(listed[0]))
	select {
	case notice := <-notices:
		require.Contains(t, notice, "Job "+job.ID+" ")
	case <-time.After(5 * time.Second):
		t.Fatal("a stop from the background list must reach the agent")
	}
}

// A job the agent stopped with job_kill sends no message: the agent knows.
func TestNativeBackgroundJobStoppedByAgentSendsNothing(t *testing.T) {
	h, sessionID, notices := nativeJobTest(t)
	job := startNativeJob(t, h, sessionID, "sleep 30")
	runNativeTool(t, h.env(sessionID), sessionID, shelltool.NewJobKillTool(), tools.JobKillParams{ShellID: job.ID})
	require.True(t, job.IsDone())
	require.Never(t, func() bool { return len(notices) > 0 }, 1500*time.Millisecond, 50*time.Millisecond, "the agent's own stop was reported")
	require.Eventually(t, func() bool { return !h.hasRunning(sessionID) }, 2*time.Second, 20*time.Millisecond)
}

// Stopping a job that has already ended leaves no mark behind.
func TestJobKillOfEndedJobLeavesNoMark(t *testing.T) {
	h, sessionID, notices := nativeJobTest(t)
	job := startNativeJob(t, h, sessionID, "sleep 1.5")
	select {
	case <-notices:
	case <-time.After(8 * time.Second):
		t.Fatal("completion notification missing")
	}
	runNativeTool(t, h.env(sessionID), sessionID, shelltool.NewJobKillTool(), tools.JobKillParams{ShellID: job.ID})
	require.False(t, tools.StoppedByAgent(job.ID))
}

// A sub-agent's turn carries no session in its shell environment, so its
// jobs aren't reported to its chat, which has finished by the time they
// end. A job whose environment names another Crush isn't taken either.
func TestJobsOutsideTheMainChatAreNotWatched(t *testing.T) {
	h, sessionID, notices := nativeJobTest(t)
	for _, env := range [][]string{
		{TasksDirEnv + "=" + h.dir, TasksSessionEnv + "="},
		{TasksDirEnv + "=" + t.TempDir(), TasksSessionEnv + "=" + sessionID},
	} {
		job, response := startJobWithEnv(t, h, env, sessionID, "sleep 30")
		require.NotContains(t, response.Content, "Crush will report when it ends")
		require.NoError(t, shell.GetBackgroundShellManager().Kill(job.ID))
	}
	require.False(t, h.hasRunning(sessionID))
	require.Never(t, func() bool { return len(notices) > 0 }, time.Second, 50*time.Millisecond)
}

// crush bg jobs go through the same watcher and still report exactly once.
func TestManagedBackgroundJobReportsOnce(t *testing.T) {
	h, sessionID, notices := nativeJobTest(t)
	response := h.background(TaskRequest{Session: sessionID, Background: &BackgroundRequest{Command: "sleep 1; printf managed", WorkingDir: t.TempDir()}})
	require.Empty(t, response.Error)
	var meta tools.BashResponseMetadata
	require.NoError(t, json.Unmarshal([]byte(response.Background.Metadata), &meta))
	t.Cleanup(func() { _ = shell.GetBackgroundShellManager().Remove(meta.ShellID) })
	select {
	case notice := <-notices:
		require.Contains(t, notice, "managed")
	case <-time.After(8 * time.Second):
		t.Fatal("completion notification missing")
	}
	require.Never(t, func() bool { return len(notices) > 0 }, time.Second, 50*time.Millisecond, "the job was reported twice")
}
