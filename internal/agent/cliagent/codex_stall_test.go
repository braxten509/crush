package cliagent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

// Codex sometimes stops getting the model's reply right after a message:
// the next tool call never arrives. Crush interrupts that turn and asks
// again, as cancelling and resending by hand does.
func TestCodexRetriesStalledReply(t *testing.T) {
	dir := t.TempDir()
	sent := filepath.Join(dir, "sent.log")
	script := `#!/bin/sh
log="` + sent + `"
read -r line
echo '{"id":"1","result":{}}'
read -r line
read -r line
echo '{"id":"2","result":{"thread":{"id":"th"}}}'
read -r line
echo '{"id":"3","result":{"turn":{"id":"tu"}}}'
echo '{"method":"item/started","params":{"item":{"id":"m1","type":"agentMessage"}}}'
echo '{"method":"item/agentMessage/delta","params":{"delta":"Looking."}}'
echo '{"method":"item/completed","params":{"item":{"id":"m1","type":"agentMessage"}}}'
while read -r line; do echo "$line" >>"$log"; case "$line" in *turn/interrupt*) break;; esac; done
echo '{"method":"turn/completed","params":{"turn":{"id":"tu","status":"interrupted"}}}'
while read -r line; do echo "$line" >>"$log"; case "$line" in *turn/start*) break;; esac; done
echo '{"id":"3","result":{"turn":{"id":"tu2"}}}'
echo '{"method":"item/started","params":{"item":{"id":"m2","type":"agentMessage"}}}'
echo '{"method":"item/agentMessage/delta","params":{"delta":"Done."}}'
echo '{"method":"item/completed","params":{"item":{"id":"m2","type":"agentMessage"}}}'
echo '{"method":"turn/completed","params":{"turn":{"id":"tu2","status":"completed"}}}'
cat >/dev/null
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	provider := NewProvider(config.TypeCodexCLI, dir, dir, nil, nil, "", false, "", false)
	model, err := provider.LanguageModel(t.Context(), "gpt-6-astra")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	var text strings.Builder
	err = model.(*Model).Run(ctx, Turn{Prompt: "test", StallRetry: 200 * time.Millisecond, Emit: func(e Event) error {
		if e.Type == EventText {
			text.WriteString(e.Text)
		}
		return nil
	}})
	require.NoError(t, err, "the stalled reply is asked again instead of hanging")
	require.Contains(t, text.String(), "Done.")
	log, _ := os.ReadFile(sent)
	require.Contains(t, string(log), `"method":"turn/start"`, "a new turn continues the work")
}
