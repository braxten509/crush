package cliagent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

// Claude reports a command it moved to the background with a task ID;
// its result carries Crush's background flag.
func TestClaudeFlagsBackgroundedCommand(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
read -r _; read -r _
echo '{"type":"system","subtype":"init","session_id":"s1"}'
echo '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"sleep 60"}},{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"ls"}}]}}'
echo '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"Command was manually backgrounded by user with ID: b1."}]},"tool_use_result":{"stdout":"","backgroundTaskId":"b1","backgroundedByUser":true}}'
echo '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t2","content":"file"}]},"tool_use_result":{"stdout":"file"}}'
echo '{"type":"result","subtype":"success"}'
cat >/dev/null
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	background := map[string]bool{}
	m := &Model{Kind: config.TypeClaudeCode, ID: "m", Dir: dir}
	require.NoError(t, m.Run(t.Context(), Turn{Prompt: "hi", Emit: func(e Event) error {
		if e.Type == EventToolResult {
			var meta tools.BashResponseMetadata
			require.NoError(t, json.Unmarshal([]byte(e.Metadata), &meta))
			background[e.ID] = meta.Background
		}
		return nil
	}}))
	require.Equal(t, map[string]bool{"t1": true, "t2": false}, background)
}
