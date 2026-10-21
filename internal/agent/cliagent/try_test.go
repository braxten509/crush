package cliagent

import (
	"testing"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

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
