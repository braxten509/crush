package cliagent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

// Codex reports a tool call only once the model has written all of it, so
// Crush infers the writing from what came before: a preamble, or thinking
// that stopped arriving.
func TestCodexReportsWritingCommand(t *testing.T) {
	quiet := codexWritingQuiet
	codexWritingQuiet = 300 * time.Millisecond
	t.Cleanup(func() { codexWritingQuiet = quiet })

	dir := t.TempDir()
	script := `#!/bin/sh
read -r line
echo '{"id":"1","result":{}}'
read -r line
read -r line
echo '{"id":"2","result":{"thread":{"id":"th"}}}'
read -r line
echo '{"id":"3","result":{"turn":{"id":"tu"}}}'
echo '{"method":"item/started","params":{"item":{"id":"m1","type":"agentMessage","phase":"commentary"}}}'
echo '{"method":"item/completed","params":{"item":{"id":"m1","type":"agentMessage","phase":"commentary"}}}'
echo '{"method":"item/started","params":{"item":{"id":"r1","type":"reasoning"}}}'
echo '{"method":"item/completed","params":{"item":{"id":"r1","type":"reasoning"}}}'
sleep 0.05
echo '{"method":"item/started","params":{"item":{"id":"r2","type":"reasoning"}}}'
echo '{"method":"item/completed","params":{"item":{"id":"r2","type":"reasoning"}}}'
sleep 1
echo '{"method":"turn/completed","params":{"turn":{"id":"tu","status":"completed"}}}'
cat >/dev/null
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	provider := NewProvider(config.TypeCodexCLI, dir, dir, nil, nil, "", false, "", false)
	model, err := provider.LanguageModel(t.Context(), "gpt-6-astra")
	require.NoError(t, err)
	var seen []string
	require.NoError(t, model.(*Model).Run(t.Context(), Turn{Prompt: "test", Emit: func(e Event) error {
		switch {
		case e.Type == EventActivity && e.Activity != "":
			seen = append(seen, e.Activity)
		case e.Type == EventReasoning:
			seen = append(seen, "thinking")
		}
		return nil
	}}))
	require.Equal(t, []string{ActivityWriting, "thinking", "thinking", ActivityWriting}, seen,
		"writing after the preamble, none between close thinking chunks, writing once thinking goes quiet")
}
