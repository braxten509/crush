package cliagent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

// shortenAutoBackground makes Crush's own cap and its check short enough to
// test.
func shortenAutoBackground(t *testing.T, after time.Duration) {
	t.Helper()
	wasAfter, wasCheck := autoBackgroundAfter, autoBackgroundCheck
	autoBackgroundAfter, autoBackgroundCheck = after, after/10
	t.Cleanup(func() { autoBackgroundAfter, autoBackgroundCheck = wasAfter, wasCheck })
}

// fakeClaudeHolding writes a claude script that asks for one Bash command,
// records every line the driver sends it, and answers the background
// request the way Claude Code answers Ctrl+B. A command it answers at once,
// keeping the turn open past the cap, is the fast case.
func fakeClaudeHolding(t *testing.T, hold bool) (stdinFile string) {
	t.Helper()
	dir := t.TempDir()
	stdinFile = filepath.Join(dir, "stdin")
	result := `echo '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}'
( sleep 0.3; echo '{"type":"result","subtype":"success"}' ) &
`
	if hold {
		result = ""
	}
	script := `#!/bin/sh
read -r _
echo '{"type":"system","subtype":"init","session_id":"s1"}'
echo '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"sleep 300"}}]}}'
( sleep 5; echo '{"type":"result","subtype":"success"}' ) &
` + result + `while read -r line; do
  printf '%s\n' "$line" >> "` + stdinFile + `"
  case "$line" in
  *background_tasks*)
    echo '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"Command was manually backgrounded by user with ID: b1."}]},"tool_use_result":{"stdout":"","backgroundTaskId":"b1","backgroundedByUser":true}}'
    echo '{"type":"result","subtype":"success"}'
    cat >/dev/null
    exit 0
    ;;
  esac
done
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return stdinFile
}

// A command a CLI waits on for the whole cap is moved to the background by
// Crush itself, with the request Ctrl+B sends.
func TestCLIMovesLongCommandToBackground(t *testing.T) {
	stdinFile := fakeClaudeHolding(t, true)
	shortenAutoBackground(t, 100*time.Millisecond)
	m := &Model{Kind: config.TypeClaudeCode, ID: "m", Dir: visibleReviewRoot(t)}
	require.NoError(t, m.Run(t.Context(), Turn{Prompt: "hi", SessionID: "s1", Emit: func(Event) error { return nil }}))
	sent, err := os.ReadFile(stdinFile)
	require.NoError(t, err)
	require.Contains(t, string(sent), `"subtype":"background_tasks"`)
}

// A command that finishes before the cap stays in the foreground: nothing is
// asked for.
func TestCLILeavesFastCommandInForeground(t *testing.T) {
	stdinFile := fakeClaudeHolding(t, false)
	shortenAutoBackground(t, 100*time.Millisecond)
	m := &Model{Kind: config.TypeClaudeCode, ID: "m", Dir: visibleReviewRoot(t)}
	require.NoError(t, m.Run(t.Context(), Turn{Prompt: "hi", SessionID: "s1", Emit: func(Event) error { return nil }}))
	sent, err := os.ReadFile(stdinFile)
	require.NoError(t, err)
	require.NotContains(t, string(sent), "background_tasks")
}
