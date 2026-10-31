package cliagent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

// A picture Claude reads comes back as an image block; its result keeps
// the picture so the chat can show it.
func TestClaudeKeepsPictureFromRead(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
read -r _; read -r _
echo '{"type":"system","subtype":"init","session_id":"s1"}'
echo '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/tmp/a.png"}}]}}'
echo '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"iVBORw0KGgo="}}]}]}}'
echo '{"type":"result","subtype":"success"}'
cat >/dev/null
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	var got Event
	m := &Model{Kind: config.TypeClaudeCode, ID: "m", Dir: dir}
	require.NoError(t, m.Run(t.Context(), Turn{Prompt: "hi", Emit: func(e Event) error {
		if e.Type == EventToolResult {
			got = e
		}
		return nil
	}}))
	require.Equal(t, tools.ViewToolName, got.Name)
	require.Equal(t, "iVBORw0KGgo=", got.Data)
	require.Equal(t, "image/png", got.MimeType)
}
