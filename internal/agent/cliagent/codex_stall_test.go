package cliagent

import (
	"bytes"
	"context"
	"encoding/json"
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

// A quiet command or patch being written must survive recovery; changing
// its wording or tool name must not make recovery cancel it. Stop still works.
func TestCodexQuietWorkSurvivesStallRecovery(t *testing.T) {
	for _, kind := range []string{"commentary", "reasoning", "command"} {
		t.Run(kind, func(t *testing.T) {
			requests := &codexRequestBuffer{}
			r := newCodexRun(t.Context(), &Model{}, Turn{Emit: func(Event) error { return nil }})
			r.p = &proc{stdin: requests}
			r.threadID = "thread"
			r.active.Store(&[2]string{"thread", "turn"})
			quiet := time.NewTimer(time.Hour)
			defer quiet.Stop()
			switch kind {
			case "commentary":
				require.NoError(t, r.handleItemCompleted(codexItem{ID: "message", Type: "agentMessage", Phase: "commentary"}))
				require.NoError(t, r.followCue(quiet))
			case "reasoning":
				require.NoError(t, r.handleItemCompleted(codexItem{ID: "thought", Type: "reasoning"}))
				require.NoError(t, r.followCue(quiet))
			case "command":
				require.NoError(t, r.handleItemStarted(codexItem{ID: "command", Type: "commandExecution", Command: "cat input"}))
			}
			// Deliver the stall timer event while that work is in progress.
			r.retryStalled()
			require.Empty(t, requests.String(), "recovery must leave ongoing work alone")
			if kind != "command" {
				// Deliver the next stall event after the writing grace expires.
				r.retryStalled()
				var recovery struct {
					Method string `json:"method"`
				}
				require.NoError(t, json.Unmarshal(requests.Bytes(), &recovery))
				require.Equal(t, "turn/interrupt", recovery.Method, "a true stall must still recover after the grace")
				requests.Reset()
			}
			r.cancel()
			var request struct {
				Method string `json:"method"`
			}
			require.NoError(t, json.Unmarshal(requests.Bytes(), &request))
			require.Equal(t, "turn/interrupt", request.Method, "explicit Stop must still interrupt")
		})
	}
}

type codexRequestBuffer struct{ bytes.Buffer }

func (*codexRequestBuffer) Close() error { return nil }
