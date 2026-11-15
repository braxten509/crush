//go:build linux

package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

// A real shell must survive automatic backgrounding, remain visible while
// the AI continues, and stop on request. Command renames and reworded replies
// must leave these outcomes intact; no live provider or user files are used.
func TestCodexAutoBackgroundKeepsCommandAndContinues(t *testing.T) {
	h := registerTestHub(t)
	hold, _ := gate(t)
	command := hold + "; true"
	encoded, err := json.Marshal(command)
	require.NoError(t, err)
	script := `#!/usr/bin/env python3
import json, os, signal, subprocess, sys
command = COMMAND_JSON
child = None
turns = 0
def emit(message):
    print(json.dumps(message), flush=True)
try:
    for raw in sys.stdin:
        req = json.loads(raw)
        method, mid = req.get("method"), req.get("id")
        if method in ("initialize", "hooks/list"):
            emit({"id": mid, "result": {"data": []}})
        elif method == "thread/start":
            emit({"id": mid, "result": {"thread": {"id": "thread"}}})
        elif method == "turn/start":
            turns += 1
            emit({"id": mid, "result": {"turn": {"id": "turn"}}})
            if turns == 1:
                child = subprocess.Popen(["bash", "-c", command], start_new_session=True)
                emit({"method": "item/started", "params": {"item": {"type": "commandExecution", "id": "command", "command": command}}})
            else:
                assert child.poll() is None, "the shell died during backgrounding"
                emit({"method": "item/agentMessage/delta", "params": {"delta": "Continuing independent work."}})
                emit({"method": "turn/completed", "params": {"turn": {"id": "turn", "status": "completed"}}})
        elif method == "turn/interrupt":
            emit({"id": mid, "result": {}})
            emit({"method": "turn/completed", "params": {"turn": {"id": "turn", "status": "interrupted"}}})
        elif method == "thread/backgroundTerminals/list":
            assert child.poll() is None, "no live background terminal"
            emit({"id": mid, "result": {"data": [{"itemId": "command", "processId": str(child.pid)}]}})
finally:
    if child is not None:
        try:
            os.killpg(child.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        child.wait()
`
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "codex"), []byte(strings.ReplaceAll(script, "COMMAND_JSON", string(encoded))), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	// Nested Crush jobs inherit the owner's job markers; they must not
	// classify this isolated fixture as part of that outer job.
	t.Setenv(tools.NativeShellEnv, "")
	t.Setenv(managedJobEnv, "")
	t.Cleanup(func() { require.NoError(t, cliagent.CloseForLock(t.Name())) })
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	backgrounded, continued := false, false
	model := &cliagent.Model{Kind: config.TypeCodexCLI, ID: "fixture", Dir: dir}
	err = model.Run(ctx, cliagent.Turn{
		SessionID: t.Name(), Prompt: "start", Env: h.env(t.Name()), AutoBackground: 50 * time.Millisecond,
		Emit: func(e cliagent.Event) error {
			if e.Type == cliagent.EventToolResult {
				var meta tools.BashResponseMetadata
				require.NoError(t, json.Unmarshal([]byte(e.Metadata), &meta))
				backgrounded = meta.Background && !e.IsError
			}
			if e.Type == cliagent.EventText && strings.TrimSpace(e.Text) != "" {
				continued = true
			}
			return nil
		},
	})
	require.NoError(t, err)
	require.True(t, backgrounded, "the command must be handed back as running")
	require.True(t, continued, "the AI must continue after the handoff")
	listed := SessionBackgroundProcesses(t.Name())
	require.Len(t, listed, 1, "the real shell must be listed once while the AI is idle")
	require.NoError(t, StopBackgroundProcess(listed[0]))
	require.Eventually(t, func() bool { return len(SessionBackgroundProcesses(t.Name())) == 0 }, time.Second, 20*time.Millisecond, "Stop must end the actual command")
}
