package cliagent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeScript puts a script named name first on PATH.
func fakeScript(t *testing.T, name, script string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func startLine(t *testing.T, lines <-chan []byte) string {
	t.Helper()
	select {
	case line := <-lines:
		return string(line)
	case <-time.After(5 * time.Second):
		t.Fatal("the fake CLI did not report how it was started")
		return ""
	}
}

// A sub-agent's Claude loads no MCP servers, claude.ai connectors
// included, unless it was asked for them.
func TestClaudeSubAgentWithoutMCP(t *testing.T) {
	dir := fakeScript(t, "claude", "read -r _\nprintf '%s|%s\\n' \"$*\" \"${ENABLE_CLAUDEAI_MCP_SERVERS:-unset}\"\ncat >/dev/null\n")
	for _, noMCP := range []bool{false, true} {
		live, err := startClaude(&Model{ID: "test", Dir: dir, Guarded: true, NoMCP: noMCP}, Turn{}, claudeKey{noMCP: noMCP})
		require.NoError(t, err)
		args, env, _ := strings.Cut(startLine(t, live.lines), "|")
		live.p.finish()
		if noMCP {
			require.Contains(t, strings.Fields(args), "--strict-mcp-config")
			require.Equal(t, "false", env)
		} else {
			require.NotContains(t, strings.Fields(args), "--strict-mcp-config")
			require.Equal(t, "unset", env)
		}
	}
}

// A sub-agent's Codex turns off every MCP server it would load, plugin ones
// included, and its ChatGPT apps.
func TestCodexSubAgentWithoutMCP(t *testing.T) {
	dir := fakeScript(t, "codex", `if [ "$1" = mcp ]; then
  printf '%s\n' '[{"name":"rea","transport":{"type":"stdio","command":"npx"}},{"name":"docs","transport":{"type":"streamable_http","url":"https://x.test/mcp"}}]'
  exit 0
fi
printf '%s\n' "$*"
cat >/dev/null
`)
	for _, noMCP := range []bool{false, true} {
		r := newCodexRun(t.Context(), &Model{ID: "test", Dir: dir, Guarded: true, NoMCP: noMCP}, Turn{})
		_, lines, err := r.open(false, codexKey{})
		require.NoError(t, err)
		args := startLine(t, lines)
		r.p.finish()
		if noMCP {
			require.Contains(t, args, "--disable apps")
			require.Contains(t, args, `-c mcp_servers.rea={command="npx",enabled=false}`)
			require.Contains(t, args, `-c mcp_servers.docs={url="https://x.test/mcp",enabled=false}`)
		} else {
			require.NotContains(t, args, "mcp_servers")
			require.NotContains(t, args, "--disable apps")
		}
	}
}

// A sub-agent's Grok skips the MCP servers it imports from other tools.
func TestGrokSubAgentWithoutMCP(t *testing.T) {
	off := []string{"GROK_CLAUDE_MCPS_ENABLED=false", "GROK_CURSOR_MCPS_ENABLED=false", "GROK_MANAGED_MCPS_ENABLED=false"}
	for _, kv := range off {
		require.Contains(t, grokEnv(&Model{NoMCP: true}, nil), kv)
		require.NotContains(t, grokEnv(&Model{}, nil), kv)
	}
}
