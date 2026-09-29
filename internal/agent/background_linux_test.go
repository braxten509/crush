package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

func TestBackgroundCountAfterCtrlB(t *testing.T) {
	h := &taskHub{dir: t.TempDir()}
	hubsMu.Lock()
	hubs = append(hubs, h)
	hubsMu.Unlock()
	t.Cleanup(func() {
		hubsMu.Lock()
		hubs = slices.DeleteFunc(hubs, func(x *taskHub) bool { return x == h })
		hubsMu.Unlock()
	})
	// A protocol fixture starts a real command, then hands it back still
	// running when Ctrl+B is requested. No real model or credentials are used.
	script := `#!/usr/bin/env python3
import json, os, signal, subprocess, sys
def emit(message):
    print(json.dumps(message), flush=True)
for raw in sys.stdin:
    if json.loads(raw).get("type") == "user":
        break
emit({"type":"system", "subtype":"init", "session_id":"native"})
emit({"type":"assistant", "message":{"content":[{"type":"tool_use", "id":"command", "name":"Bash", "input":{"command":"sleep 30; true"}}]}})
child = subprocess.Popen(["bash", "-c", "sleep 30; true"], start_new_session=True)
emit({"type":"system", "subtype":"task_started", "tool_use_id":"command"})
try:
    for raw in sys.stdin:
        request = json.loads(raw).get("request", {}).get("subtype")
        if request == "background_tasks":
            emit({"type":"user", "message":{"content":[{"type":"tool_result", "tool_use_id":"command", "content":"Moved to the background"}]}})
        elif request == "interrupt":
            emit({"type":"result", "subtype":"success"})
            break
finally:
    try:
        os.killpg(child.pid, signal.SIGKILL)
    except ProcessLookupError:
        pass
    child.wait()
`
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	model := &cliagent.Model{Kind: config.TypeClaudeCode, ID: "fixture", Dir: dir, Guarded: true}
	go func() {
		done <- model.Run(ctx, cliagent.Turn{
			SessionID: t.Name(), Prompt: "start",
			Env:  []string{TasksDirEnv + "=" + h.dir},
			Emit: func(cliagent.Event) error { return nil },
		})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("fixture did not stop")
		}
	})
	require.Eventually(t, func() bool {
		procs := markedProcs(hubMarkers())
		for _, p := range procs {
			if isCommandRoot(p, procs) && commandText(p.args) == "sleep 30; true" {
				return true
			}
		}
		return false
	}, 5*time.Second, 20*time.Millisecond)
	require.Empty(t, BackgroundProcesses(), "the foreground command must not count as background")
	require.True(t, cliagent.Background(t.Name()))
	require.Eventually(t, func() bool {
		procs := BackgroundProcesses()
		return len(procs) == 1 && procs[0].Command == "sleep 30; true"
	}, 5*time.Second, 20*time.Millisecond, "Ctrl+B adds the still-running command to the background list")
}

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
	require.Equal(t, found[0].Started, BackgroundProcesses()[0].Started, "start times remain stable for process identity checks")

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
