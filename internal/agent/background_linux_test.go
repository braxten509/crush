package agent

import (
	"os"
	"os/exec"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBackgroundProcesses(t *testing.T) {
	h := &taskHub{dir: t.TempDir()}
	hubsMu.Lock()
	hubs = append(hubs, h)
	hubsMu.Unlock()
	t.Cleanup(func() {
		hubsMu.Lock()
		hubs = slices.DeleteFunc(hubs, func(x *taskHub) bool { return x == h })
		hubsMu.Unlock()
	})

	// A stand-in CLI (not a shell) that runs a command through a shell,
	// like an agent's bash tool does.
	cli := exec.Command("python3", "-c", `import subprocess; subprocess.run(["bash", "-c", "sleep 30; true"])`)
	cli.Env = append(os.Environ(), TasksDirEnv+"="+h.dir)
	require.NoError(t, cli.Start())
	t.Cleanup(func() { _ = cli.Process.Kill(); _ = cli.Wait() })

	var found []Process
	require.Eventually(t, func() bool {
		found = BackgroundProcesses()
		return len(found) == 1
	}, 5*time.Second, 50*time.Millisecond, "the CLI itself and the sleep under the shell are not listed")
	require.Equal(t, "sleep 30; true", found[0].Command)

	require.NoError(t, KillProcess(found[0].PID))
	require.Eventually(t, func() bool { return len(BackgroundProcesses()) == 0 }, 5*time.Second, 50*time.Millisecond)
	require.Error(t, KillProcess(os.Getpid()), "only listed processes can be killed")
}

func TestCommandText(t *testing.T) {
	t.Parallel()
	require.Equal(t, "npm run dev", commandText([]string{"/bin/zsh", "-c", `source /x/snapshot.sh 2>/dev/null || true && eval 'npm run dev' \< /dev/null && pwd -P >| /tmp/c`}))
	require.Equal(t, "echo 'hi'", commandText([]string{"bash", "-c", `eval 'echo '\''hi'\''' \< /dev/null`}))
	require.Equal(t, "go test ./...", commandText([]string{"/bin/bash", "-lc", "go test ./..."}))
	require.Equal(t, "node server.js", commandText([]string{"node", "server.js"}))
}
