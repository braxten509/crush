package agent

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// When Crush dies without its shutdown, the reaper stops what Crush
// started, even a command that detached into its own session, and leaves
// D-Bus-started services alone.
func TestReaperStopsMarkedProcessesWhenCrushDies(t *testing.T) {
	marker := TasksDirEnv + "=/reaper-test-" + uuid.NewString()
	start := func(env ...string) *exec.Cmd {
		cmd := exec.Command("sleep", "60")
		cmd.Env = append(os.Environ(), env...)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		require.NoError(t, cmd.Start())
		t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
		return cmd
	}
	job := start(marker)
	ignores := exec.Command("sh", "-c", `trap "" TERM; echo ready; exec sleep 60`)
	ignores.Env = append(os.Environ(), marker)
	ignores.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	out, err := ignores.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, ignores.Start())
	t.Cleanup(func() { _ = ignores.Process.Kill(); _ = ignores.Wait() })
	_, err = out.Read(make([]byte, 6))
	require.NoError(t, err)
	bus := start(marker, "DBUS_STARTER_BUS_TYPE=session")
	other := start()

	owner, err := startReaper(marker)
	require.NoError(t, err)
	// Crush dying closes its end of the pipe.
	require.NoError(t, owner.Close())

	exited := func(cmd *exec.Cmd) func() bool {
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		return func() bool {
			select {
			case <-done:
				return true
			default:
				return false
			}
		}
	}
	jobDone, ignoresDone := exited(job), exited(ignores)
	require.Eventually(t, jobDone, 5*time.Second, 20*time.Millisecond, "the reaper didn't stop the job")
	require.Eventually(t, ignoresDone, reapGrace+5*time.Second, 20*time.Millisecond, "the reaper didn't kill a job ignoring SIGTERM")
	require.True(t, running(bus.Process.Pid), "the reaper stopped a D-Bus service")
	require.True(t, running(other.Process.Pid), "the reaper stopped an unmarked process")
}

// While Crush lives, the reaper waits.
func TestReaperWaitsWhileCrushLives(t *testing.T) {
	marker := TasksDirEnv + "=/reaper-test-" + uuid.NewString()
	job := exec.Command("sleep", "60")
	job.Env = append(os.Environ(), marker)
	require.NoError(t, job.Start())
	t.Cleanup(func() { _ = job.Process.Kill(); _ = job.Wait() })
	owner, err := startReaper(marker)
	require.NoError(t, err)
	t.Cleanup(func() { _ = owner.Close() })
	time.Sleep(500 * time.Millisecond)
	require.True(t, running(job.Process.Pid))
}

// running reports whether pid is alive and not a zombie waiting to be
// collected, which a signal still reaches.
func running(pid int) bool {
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	fields := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))
	return len(fields) > 0 && fields[0] != "Z"
}
