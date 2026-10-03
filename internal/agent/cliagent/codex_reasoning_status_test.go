package cliagent

import (
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestCodexReasoningLifecycleWithoutTextShowsActivity(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
read -r line
echo '{"id":"1","result":{}}'
read -r line
read -r line
echo '{"id":"2","result":{"thread":{"id":"th"}}}'
read -r line
echo '{"id":"3","result":{"turn":{"id":"tu"}}}'
echo '{"method":"item/started","params":{"item":{"id":"reasoning","type":"reasoning"}}}'
echo '{"method":"turn/completed","params":{"turn":{"id":"tu","status":"completed"}}}'
cat >/dev/null
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	provider := NewProvider(config.TypeCodexCLI, dir, dir, nil, nil, "", false)
	model, err := provider.LanguageModel(t.Context(), "gpt-6-astra")
	require.NoError(t, err)
	var activity []Event
	heartbeats := 0
	require.NoError(t, model.(*Model).Run(t.Context(), Turn{Prompt: "test", Emit: func(e Event) error {
		if e.Type == EventReasoning {
			activity = append(activity, e)
		}
		if e.Type == EventActivity {
			heartbeats++
		}
		return nil
	}}))
	require.Len(t, activity, 1)
	require.Empty(t, activity[0].Text, "only activity metadata is emitted")
	require.GreaterOrEqual(t, heartbeats, 1)
}
