package cliagent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

// TestOpenCodeSteersOnlyWhileAToolRuns: OpenCode takes a session/prompt sent
// mid-turn at its next step only while a tool runs. Sent while the model
// writes, it waits for the turn to end, so the user had to interrupt.
func TestOpenCodeSteersOnlyWhileAToolRuns(t *testing.T) {
	bin := t.TempDir()
	marker := filepath.Join(t.TempDir(), "marker.json")
	script := `#!/usr/bin/env python3
import json, os, select, sys, time

def send(m):
    m["jsonrpc"] = "2.0"
    print(json.dumps(m), flush=True)

def read_line(timeout=None):
    if timeout is not None:
        ready, _, _ = select.select([sys.stdin], [], [], timeout)
        if not ready:
            return None
    line = sys.stdin.readline()
    if not line:
        return None
    return json.loads(line)

def wait_prompt(timeout):
    end = time.time() + timeout
    while time.time() < end:
        m = read_line(max(0.01, end - time.time()))
        if not m:
            continue
        if m.get("method") == "session/prompt" and m.get("id") != "3":
            return m
    return None

read_line()
send({"id": "1", "result": {"protocolVersion": 1}})
while True:
    m = read_line()
    if m.get("id") == "2":
        send({"id": "2", "result": {"sessionId": "ses1"}})
        break
while True:
    m = read_line()
    if m.get("id") == "3":
        break
early = wait_prompt(0.35)
send({"method": "session/update", "params": {"sessionId": "ses1", "update": {"sessionUpdate": "tool_call", "toolCallId": "c1", "title": "bash", "kind": "execute", "status": "in_progress", "rawInput": {"command": "true"}}}})
after = wait_prompt(2.0)
send({"method": "session/update", "params": {"sessionId": "ses1", "update": {"sessionUpdate": "tool_call_update", "toolCallId": "c1", "status": "completed", "content": [{"type": "content", "content": {"type": "text", "text": ""}}]}}})
send({"id": "3", "result": {"stopReason": "end_turn"}})
if after:
    send({"id": after["id"], "result": {"stopReason": "end_turn"}})
with open(os.environ["STEER_MARKER"], "w") as f:
    json.dump({"early": early is not None, "after": after is not None}, f)
sys.stdin.read()
`
	require.NoError(t, os.WriteFile(filepath.Join(bin, "opencode"), []byte(script), 0o755))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var once sync.Once
	steer := func() string {
		text := ""
		once.Do(func() { text = "QUEUED" })
		return text
	}
	m := &Model{Kind: config.TypeOpenCodeCLI, ID: "opencode-go/deepseek-v4.1-flash", Dir: t.TempDir()}
	done := make(chan error, 1)
	go func() {
		done <- m.Run(context.Background(), Turn{
			SessionID: "s", Prompt: "hi", Steer: steer,
			Env:  []string{"STEER_MARKER=" + marker},
			Emit: func(Event) error { return nil },
		})
	}()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("the turn did not finish")
	}

	data, err := os.ReadFile(marker)
	require.NoError(t, err)
	var got struct {
		Early bool `json:"early"`
		After bool `json:"after"`
	}
	require.NoError(t, json.Unmarshal(data, &got))
	require.False(t, got.Early, "a queued prompt must wait for a running tool")
	require.True(t, got.After, "a queued prompt goes while the tool runs")
}
