package cliagent

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

func TestClaudeSteersAtNextToolResult(t *testing.T) {
	// The first tool returns before the next poll. A queued message must
	// already be on stdin then, or Claude makes another tool call first.
	dir := t.TempDir()
	script := `#!/bin/sh
read -r _; read -r _
echo '{"type":"system","subtype":"init","session_id":"s1"}'
echo '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"echo first"}}]}}'
(
  sleep 0.1
  echo '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"first"}]}}'
  if [ ! -f "$CRUSH_TEST_STEER_RECEIVED" ]; then
    echo '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"echo too-late"}}]}}'
    sleep 0.3
  fi
  echo '{"type":"user","message":{"content":"PINEAPPLE"},"isReplay":true}'
  echo '{"type":"result","subtype":"success"}'
) &
read -r steer
case "$steer" in *PINEAPPLE*) ;; *) exit 1;; esac
touch "$CRUSH_TEST_STEER_RECEIVED"
wait
cat >/dev/null
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	ready := make(chan struct{}, 1)
	var queued atomic.Bool
	var events []Event
	m := &Model{Kind: config.TypeClaudeCode, ID: "m", Dir: dir}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	require.NoError(t, m.Run(ctx, Turn{
		Prompt: "hi",
		Env:    []string{"CRUSH_TEST_STEER_RECEIVED=" + filepath.Join(dir, "received")},
		Emit: func(e Event) error {
			events = append(events, e)
			if e.Type == EventToolCall && e.ID == "t1" {
				queued.Store(true)
				ready <- struct{}{}
			}
			return nil
		},
		Steer: func() string {
			if queued.Swap(false) {
				return "PINEAPPLE"
			}
			return ""
		},
		SteerReady: ready,
	}))
	require.Equal(t, []EventType{EventSession, EventToolCall, EventToolResult, EventUserMessage}, types(events))
	require.Equal(t, "PINEAPPLE", events[3].Text)
}

func TestSteerReadyRespectsReadinessAndStop(t *testing.T) {
	ready := make(chan struct{}, 1)
	var active atomic.Bool
	sent := make(chan string, 1)
	stop := pollSteer(Turn{
		Steer:      func() string { return "queued" },
		SteerReady: ready,
	}, active.Load, func(text string) { sent <- text })
	t.Cleanup(stop)
	ready <- struct{}{}
	select {
	case <-sent:
		t.Fatal("steered before the CLI was ready")
	case <-time.After(20 * time.Millisecond):
	}
	active.Store(true)
	select {
	case text := <-sent:
		require.Equal(t, "queued", text, "polling must recover a wake received before readiness")
	case <-time.After(time.Second):
		t.Fatal("pending prompt was not retried")
	}
	stop()
	stop()
	ready <- struct{}{}
	select {
	case <-sent:
		t.Fatal("steered after stop returned")
	case <-time.After(20 * time.Millisecond):
	}
}
