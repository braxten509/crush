package cliagent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/charmbracelet/crush/internal/agent/tools"

	"github.com/stretchr/testify/require"
)

func TestCodexShortParallelCommandsKeepResults(t *testing.T) {
	fakeCLI(t, "codex", `{"id":"1","result":{}}
{"id":"2","result":{"thread":{"id":"th1"}}}
{"id":"3","result":{"turn":{"id":"tu1"}}}
{"method":"item/started","params":{"item":{"type":"commandExecution","id":"c1","command":"echo first"}}}
{"method":"item/started","params":{"item":{"type":"commandExecution","id":"c2","command":"echo second"}}}
{"method":"item/started","params":{"item":{"type":"agentMessage","id":"m1"}}}
{"method":"item/completed","params":{"item":{"type":"commandExecution","id":"c1","status":"completed","aggregatedOutput":"first\n","exitCode":0}}}
{"method":"item/completed","params":{"item":{"type":"commandExecution","id":"c2","status":"failed","aggregatedOutput":"second failed\n","exitCode":1}}}
{"method":"turn/completed","params":{"turn":{"id":"tu1","status":"completed"}}}
`)
	events, err := collect(t, "codex", "")
	require.NoError(t, err)
	var results []Event
	for _, event := range events {
		if event.Type == EventToolResult {
			results = append(results, event)
		}
	}
	require.Len(t, results, 2)
	require.Equal(t, "first\n", results[0].Output)
	require.False(t, results[0].IsError)
	require.Contains(t, results[1].Output, "second failed")
	require.True(t, results[1].IsError)
}

// A handoff must report a running job only when Codex confirms its terminal;
// changing the command or explanatory wording must leave this contract intact.
func TestCodexBackgroundHandoffRequiresConfirmedTerminal(t *testing.T) {
	for _, outcome := range []string{"running", "ended", "unavailable", "unrelated", "partial"} {
		t.Run(outcome, func(t *testing.T) {
			requests := &codexRequestBuffer{}
			var results []Event
			r := newCodexRun(context.Background(), &Model{}, Turn{Emit: func(e Event) error {
				if e.Type == EventToolResult {
					results = append(results, e)
				}
				return nil
			}})
			r.p = &proc{stdin: requests}
			r.threadID = "thread"
			r.active.Store(&[2]string{"thread", "turn"})
			require.NoError(t, r.handleItemStarted(codexItem{ID: "command", Type: "commandExecution", Command: "cat input"}))
			if outcome == "partial" {
				require.NoError(t, r.handleItemStarted(codexItem{ID: "uncertain-command", Type: "commandExecution", Command: "cat another-input"}))
			}
			r.movedToBg.Store(true)
			var params codexParams
			params.Turn.ID, params.Turn.Status = "turn", "interrupted"
			done, err := r.handleTurnCompleted(params)
			require.NoError(t, err)
			require.False(t, done, "backgrounding must continue the AI's work")
			require.Empty(t, results, "an interrupt alone is no proof the command survived")
			msg := rpcMessage{Result: json.RawMessage(`{"data":[]}`)}
			if outcome == "running" || outcome == "partial" {
				msg.Result = json.RawMessage(`{"data":[{"itemId":"command","processId":"terminal"}]}`)
			} else if outcome == "unrelated" {
				msg.Result = json.RawMessage(`{"data":[{"itemId":"old-command","processId":"old-terminal"}]}`)
			} else if outcome == "unavailable" {
				require.NoError(t, json.Unmarshal([]byte(`{"error":{"code":-32601,"message":"unavailable"}}`), &msg))
			}
			require.NoError(t, r.handleResponse("ctrl-b-list", msg))
			wantResults := 1
			if outcome == "partial" {
				wantResults++
			}
			require.Len(t, results, wantResults)
			for _, result := range results {
				var meta tools.BashResponseMetadata
				if result.Metadata != "" {
					require.NoError(t, json.Unmarshal([]byte(result.Metadata), &meta))
				}
				confirmed := result.ID == "command" && (outcome == "running" || outcome == "partial")
				require.Equal(t, confirmed, meta.Background)
				require.Equal(t, !confirmed, result.IsError)
				require.NoError(t, r.handleItemCompleted(codexItem{ID: result.ID, Type: "commandExecution", Status: "completed"}))
			}
			require.Len(t, results, wantResults, "late completion must not append a second result")
			decoder := json.NewDecoder(requests)
			continued := false
			for decoder.More() {
				var request struct {
					Method string `json:"method"`
					Params struct {
						Input []struct {
							Text string `json:"text"`
						} `json:"input"`
					} `json:"params"`
				}
				require.NoError(t, decoder.Decode(&request))
				continued = continued || request.Method == "turn/start"
				if request.Method == "turn/start" && outcome == "partial" {
					require.Len(t, request.Params.Input, 1)
					require.Contains(t, request.Params.Input[0].Text, "uncertain-command", "the AI must be told which parallel command was not confirmed")
				}
			}
			require.True(t, continued, "the AI must continue even when a command has ended")
		})
	}
}
