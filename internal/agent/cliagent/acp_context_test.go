package cliagent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

// TestOpenCodeReportsTheContextInUse: OpenCode's prompt result sums every
// step of the turn, so it cannot be the context meter's used count. Its
// usage_update carries the last step's total; that value must win.
func TestOpenCodeReportsTheContextInUse(t *testing.T) {
	bin := t.TempDir()
	script := `#!/usr/bin/env python3
import json, sys

def send(m):
    m["jsonrpc"] = "2.0"
    print(json.dumps(m), flush=True)

for line in sys.stdin:
    m = json.loads(line)
    if m.get("id") == "1":
        send({"id": "1", "result": {"protocolVersion": 1}})
    elif m.get("id") == "2":
        send({"id": "2", "result": {"sessionId": "ses1"}})
    elif m.get("id") == "3":
        send({"method": "session/update", "params": {"sessionId": "ses1", "update": {"sessionUpdate": "usage_update", "used": 54321, "size": 200000}}})
        send({"id": "3", "result": {"stopReason": "end_turn", "usage": {"inputTokens": 999999, "outputTokens": 50}}})
        break
sys.stdin.read()
`
	require.NoError(t, os.WriteFile(filepath.Join(bin, "opencode"), []byte(script), 0o755))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var events []Event
	m := &Model{Kind: config.TypeOpenCodeCLI, ID: "opencode-go/deepseek-v4.1-flash", Dir: t.TempDir()}
	require.NoError(t, m.Run(context.Background(), Turn{
		SessionID: "s", Prompt: "hi",
		Emit: func(e Event) error {
			events = append(events, e)
			return nil
		},
	}))

	usageAt, contextAt := -1, -1
	var last Event
	for i, e := range events {
		switch e.Type {
		case EventUsage:
			usageAt = i
		case EventContextBudget:
			contextAt = i
			last = e
		}
	}
	require.Greater(t, usageAt, -1, "the turn reports its usage")
	require.Greater(t, contextAt, usageAt, "the context in use is reported again after the turn's usage")
	require.EqualValues(t, 54321, last.ContextUsed)
	require.EqualValues(t, 168000, last.ContextLimit)
}
