package cliagent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/stretchr/testify/require"
)

// Grok runs its commands in one long-lived shell, so Crush could not tie
// their changes to a command. Offered ACP's terminal, Grok asks Crush to
// run each command, and the files it changes become that call's record.
func TestGrokCommandRunByCrushIsRecorded(t *testing.T) {
	bin := t.TempDir()
	script := `#!/usr/bin/env python3
import json, sys
def send(m):
    m["jsonrpc"] = "2.0"
    print(json.dumps(m), flush=True)
def recv(want):
    for line in sys.stdin:
        m = json.loads(line)
        if m.get("id") == want:
            return m
    sys.exit("no answer to " + want)
init = recv("1")
if not init["params"]["clientCapabilities"].get("terminal"):
    sys.exit("Crush did not offer its terminal")
send({"id": "1", "result": {"protocolVersion": 1}})
recv("2")
send({"id": "2", "result": {"sessionId": "ses1"}})
prompt = recv("3")
def update(u):
    send({"method": "session/update", "params": {"sessionId": "ses1", "update": u}})
update({"sessionUpdate": "tool_call", "toolCallId": "c1", "title": "run_terminal_command", "kind": "execute", "status": "in_progress", "rawInput": {"command": "printf hi > made.txt"}})
send({"id": "t1", "method": "terminal/create", "params": {"sessionId": "ses1", "command": "printf hi > made.txt"}})
term = recv("t1")["result"]["terminalId"]
update({"sessionUpdate": "tool_call_update", "toolCallId": "c1", "content": [{"type": "terminal", "terminalId": term}]})
send({"id": "t2", "method": "terminal/wait_for_exit", "params": {"sessionId": "ses1", "terminalId": term}})
assert recv("t2")["result"]["exitCode"] == 0
send({"id": "t3", "method": "terminal/release", "params": {"sessionId": "ses1", "terminalId": term}})
recv("t3")
update({"sessionUpdate": "tool_call_update", "toolCallId": "c1", "status": "completed", "content": [{"type": "content", "content": {"type": "text", "text": ""}}]})
send({"id": "3", "result": {"stopReason": "end_turn"}})
sys.stdin.read()
`
	require.NoError(t, os.WriteFile(filepath.Join(bin, "grok"), []byte(script), 0o755))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	var events []Event
	m := &Model{Kind: config.TypeGrokCLI, ID: "m", Dir: visibleReviewRoot(t)}
	err := m.Run(context.Background(), Turn{Prompt: "hi", Emit: func(e Event) error {
		events = append(events, e)
		return nil
	}})
	require.NoError(t, err)
	data, err := os.ReadFile(filepath.Join(m.Dir, "made.txt"))
	require.NoError(t, err)
	require.Equal(t, "hi", string(data))
	if err := filechange.WatchUnavailable(); err != nil {
		t.Skipf("commands cannot be watched here (run outside a Crush command): %v", err)
	}
	var review *filechange.Review
	for _, e := range events {
		if e.Type == EventToolResult && e.ID == "c1" {
			_, review = filechange.TakeReview(e.Metadata)
		}
	}
	require.NotNil(t, review, "the command's changes reach the call's record")
	require.Len(t, review.Changes, 1)
	require.Equal(t, filepath.Join(m.Dir, "made.txt"), review.Changes[0].Path)
	require.Equal(t, "hi", review.Changes[0].After.Content)
}

// A terminal keeps only the newest bytes of its output, up to the limit
// the agent asked for.
func TestACPTerminalOutputKeepsTheNewestBytes(t *testing.T) {
	terms := newACPTerminals(t.TempDir())
	defer terms.closeAll()
	ask := func(method string, params map[string]any) map[string]any {
		raw, err := json.Marshal(params)
		require.NoError(t, err)
		result, err := terms.handle(context.Background(), method, raw)
		require.NoError(t, err)
		data, err := json.Marshal(result)
		require.NoError(t, err)
		var out map[string]any
		require.NoError(t, json.Unmarshal(data, &out))
		return out
	}
	created := ask("terminal/create", map[string]any{"command": "printf", "args": []string{"0123456789"}, "outputByteLimit": 4})
	id := created["terminalId"]
	ask("terminal/wait_for_exit", map[string]any{"terminalId": id})
	out := ask("terminal/output", map[string]any{"terminalId": id})
	require.Equal(t, "6789", out["output"])
	require.Equal(t, true, out["truncated"])
	require.Equal(t, map[string]any{"exitCode": float64(0), "signal": nil}, out["exitStatus"])
}
