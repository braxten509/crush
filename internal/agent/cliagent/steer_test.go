package cliagent

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

// steerTurn runs a turn whose Steer hands over "PINEAPPLE" once.
func steerTurn(t *testing.T, kind string, script string) []Event {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, kind), []byte("#!/bin/sh\n"+script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	m := &Model{Kind: map[string]catwalk.Type{"claude": config.TypeClaudeCode, "codex": config.TypeCodexCLI}[kind], ID: "m", Dir: t.TempDir()}
	var given atomic.Bool
	var events []Event
	err := m.Run(context.Background(), Turn{
		Prompt: "hi",
		Emit:   func(e Event) error { events = append(events, e); return nil },
		Steer: func() string {
			if given.Swap(true) {
				return ""
			}
			return "PINEAPPLE"
		},
	})
	require.NoError(t, err)
	return events
}

func TestClaudeSteer(t *testing.T) {
	// The steered message lands as the model finishes, so Claude answers
	// it as a follow-up after its first result; the driver waits for it.
	events := steerTurn(t, "claude", `read -r _; read -r _
echo '{"type":"system","subtype":"init","session_id":"s1"}'
read -r steer
case "$steer" in *PINEAPPLE*) ;; *) exit 1;; esac
echo '{"type":"result","subtype":"success","result":"first"}'
echo '{"type":"user","message":{"role":"user","content":"PINEAPPLE"},"isReplay":true}'
echo '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"second"}}}'
echo '{"type":"result","subtype":"success","result":"second"}'
cat >/dev/null
`)
	require.Equal(t, []EventType{EventSession, EventUserMessage, EventText}, types(events))
	require.Equal(t, "PINEAPPLE", events[1].Text)
}

func TestCodexSteer(t *testing.T) {
	events := steerTurn(t, "codex", `read -r _; echo '{"id":"1","result":{}}'
read -r _; read -r _; echo '{"id":"2","result":{"thread":{"id":"th"}}}'
read -r _; echo '{"id":"3","result":{"turn":{"id":"tu"}}}'
read -r steer
case "$steer" in *turn/steer*expectedTurnId*tu*PINEAPPLE*|*PINEAPPLE*expectedTurnId*tu*|*expectedTurnId*tu*PINEAPPLE*) ;; *) exit 1;; esac
echo '{"id":"steer-1","result":{"turnId":"tu"}}'
echo '{"method":"item/started","params":{"item":{"type":"userMessage","id":"u2","content":[{"type":"text","text":"PINEAPPLE"}]}}}'
echo '{"method":"turn/completed","params":{"turn":{"id":"tu","status":"completed"}}}'
cat >/dev/null
`)
	require.Equal(t, []EventType{EventSession, EventUserMessage}, types(events))
	require.Equal(t, "PINEAPPLE", events[1].Text)
}

func TestClaudeSteersDuringFollowUp(t *testing.T) {
	// Each message arrives during the reply to the previous one. Claude
	// takes it after that reply's result, without ending the Crush turn.
	dir := t.TempDir()
	script := `#!/bin/sh
read -r _; read -r _
echo '{"type":"system","subtype":"init","session_id":"s1"}'
for text in FIRST SECOND THIRD; do
  read -r steer
  case "$steer" in *"$text"*) ;; *) exit 1;; esac
  echo '{"type":"result","subtype":"success"}'
  echo "{\"type\":\"user\",\"message\":{\"content\":\"$text\"},\"isReplay\":true}"
done
echo '{"type":"result","subtype":"success"}'
cat >/dev/null
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	m := &Model{Kind: config.TypeClaudeCode, ID: "m", Dir: t.TempDir()}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var acknowledged atomic.Int32
	var confirmed []string
	next := 0
	prompts := []string{"FIRST", "SECOND", "THIRD"}
	err := m.Run(ctx, Turn{
		Prompt: "hi",
		Emit: func(e Event) error {
			if e.Type == EventUserMessage {
				confirmed = append(confirmed, e.Text)
				acknowledged.Add(1)
			}
			return nil
		},
		Steer: func() string {
			if next == len(prompts) || next > int(acknowledged.Load()) {
				return ""
			}
			text := prompts[next]
			next++
			return text
		},
	})
	require.NoError(t, err)
	require.Equal(t, prompts, confirmed)
}

func TestClaudeKeepsProcess(t *testing.T) {
	// One process answers each prompt it reads; every start is logged.
	dir := t.TempDir()
	starts := filepath.Join(dir, "starts")
	script := `echo start >> ` + starts + `
read -r _
while read -r _; do
echo '{"type":"system","subtype":"init","session_id":"s1"}'
echo '{"type":"result","subtype":"success","result":"ok"}'
done
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "claude"), []byte("#!/bin/sh\n"+script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	m := &Model{Kind: config.TypeClaudeCode, ID: "m", Dir: t.TempDir()}
	turn := func(resume string) {
		require.NoError(t, m.Run(context.Background(), Turn{SessionID: "keep-test", Prompt: "hi", Resume: resume, Emit: func(Event) error { return nil }}))
	}
	turn("")
	turn("s1")
	m.ID = "other" // A different model needs its own process.
	turn("s1")
	takeClaude("keep-test", claudeKey{}, "") // Close the parked one.

	data, err := os.ReadFile(starts)
	require.NoError(t, err)
	require.Equal(t, "start\nstart\n", string(data))
}

func TestClaudeKeptWhileTaskRuns(t *testing.T) {
	// The task ends on its own 400ms after the turn; the idle limit is 100ms.
	dir := t.TempDir()
	script := `read -r _
read -r _
echo '{"type":"system","subtype":"init","session_id":"s1"}'
echo '{"type":"system","subtype":"task_started","task_id":"t1","is_backgrounded":true}'
echo '{"type":"result","subtype":"success","result":"ok"}'
sleep 0.4
echo '{"type":"system","subtype":"task_notification","task_id":"t1","status":"completed"}'
while read -r _; do :; done
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "claude"), []byte("#!/bin/sh\n"+script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	idle := claudeIdle
	claudeIdle = 100 * time.Millisecond
	t.Cleanup(func() { claudeIdle = idle })
	m := &Model{Kind: config.TypeClaudeCode, ID: "m", Dir: t.TempDir()}
	require.NoError(t, m.Run(context.Background(), Turn{SessionID: "task-test", Prompt: "hi", Emit: func(Event) error { return nil }}))
	parked := func() bool {
		claudeSessions.mu.Lock()
		defer claudeSessions.mu.Unlock()
		return claudeSessions.m["task-test"] != nil
	}
	time.Sleep(250 * time.Millisecond)
	require.True(t, parked(), "closed while its task was running")
	require.Eventually(t, func() bool { return !parked() }, 3*time.Second, 20*time.Millisecond, "kept after its task ended")
}
