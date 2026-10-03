package cliagent

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

func TestCodexAttachments(t *testing.T) {
	for _, resume := range []string{"", "existing-thread"} {
		t.Run(resume, func(t *testing.T) {
			dir := t.TempDir()
			script := `#!/bin/sh
read -r line
echo '{"id":"1","result":{}}'
read -r line
read -r line
echo '{"id":"2","result":{"thread":{"id":"th"}}}'
read -r line
printf '%s\n' "$line" > turn.json
echo '{"id":"3","result":{"turn":{"id":"tu"}}}'
echo '{"method":"turn/completed","params":{"turn":{"id":"tu","status":"completed"}}}'
cat >/dev/null
`
			require.NoError(t, os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0o755))
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aXioAAAAASUVORK5CYII=")
			require.NoError(t, err)
			m := &Model{Kind: config.TypeCodexCLI, ID: "gpt-6-astra", Dir: dir}
			require.NoError(t, m.Run(t.Context(), Turn{
				Prompt: "What is in these images?", Resume: resume,
				Attachments: []message.Attachment{
					{FilePath: "/no-longer-exists.png", MimeType: "image/png", Content: png},
					{FileName: "clipboard.png", MimeType: "image/png", Content: png},
					{MimeType: "text/plain", Content: []byte("already in prompt")},
				},
				Emit: func(Event) error { return nil },
			}))
			data, err := os.ReadFile(filepath.Join(dir, "turn.json"))
			require.NoError(t, err)
			var request struct {
				Method string `json:"method"`
				Params struct {
					Input []map[string]any `json:"input"`
				} `json:"params"`
			}
			require.NoError(t, json.Unmarshal(data, &request))
			require.Equal(t, "turn/start", request.Method)
			require.Len(t, request.Params.Input, 3)
			text := request.Params.Input[0]["text"].(string)
			require.Equal(t, "What is in these images?", withoutImagePaths(text))
			require.Contains(t, text, filepath.Join(os.Getenv("XDG_CACHE_HOME"), "crush", "attachments"))
			for _, part := range request.Params.Input[1:] {
				require.Equal(t, "image", part["type"])
				require.Equal(t, "data:image/png;base64,"+base64.StdEncoding.EncodeToString(png), part["url"])
			}
		})
	}
}
