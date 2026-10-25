package cliagent

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

// A tool whose child keeps its output open must not hold the try past its
// time limit.
func TestTryAccountStopsWithItsChildren(t *testing.T) {
	previous := tryTimeout
	tryTimeout = 200 * time.Millisecond
	t.Cleanup(func() { tryTimeout = previous })
	dir := t.TempDir()
	pids := filepath.Join(dir, "pids")
	script := "#!/bin/sh\ntail -f /dev/null &\necho $! > \"$CRUSH_TRY_PIDS\"\nexec tail -f /dev/null\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CRUSH_TRY_PIDS", pids)
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(func() {
		data, _ := os.ReadFile(pids)
		if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
			if p, err := os.FindProcess(pid); err == nil {
				_ = p.Kill()
			}
		}
	})

	done := make(chan error, 1)
	go func() { done <- TryAccount(t.Context(), config.TypeClaudeCode, "", "") }()
	select {
	case err := <-done:
		require.ErrorContains(t, err, "did not answer")
	case <-time.After(3 * time.Second):
		t.Fatal("TryAccount waited on a child that kept its output open")
	}
}

// A failed hidden request is told by the tool's own words, as each prints
// them (shapes taken from Claude Code and Codex on 2026-10-21), and a
// request that worked has none, so the fallback order never takes a dead
// account for a working one.
func TestTryFailureReadsEachToolsWords(t *testing.T) {
	claude := "[claude-code:unrecognized_model] {\"model\":\"x\"}\n" +
		`{"type":"result","subtype":"success","is_error":true,"result":"Claude AI usage limit reached|1761062400"}`
	require.Equal(t, "Claude AI usage limit reached|1761062400", tryFailure(config.TypeClaudeCode, []byte(claude)))
	require.Empty(t, tryFailure(config.TypeClaudeCode, []byte(`{"type":"result","subtype":"success","is_error":false,"result":"OK"}`)))

	codex := `{"type":"thread.started","thread_id":"t"}` + "\n" + `{"type":"turn.started"}` + "\n" +
		`{"type":"error","message":"reconnecting"}` + "\n" +
		`{"type":"turn.failed","error":{"message":"You've hit your usage limit. Try again at 3:05 PM."}}`
	require.Equal(t, "You've hit your usage limit. Try again at 3:05 PM.", tryFailure(config.TypeCodexCLI, []byte(codex)))
	require.Empty(t, tryFailure(config.TypeCodexCLI, []byte(`{"type":"turn.started"}`+"\n"+`{"type":"turn.completed","usage":{}}`)))
}
