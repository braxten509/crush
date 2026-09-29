package cliagent

import (
	"testing"

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
	require.Equal(t, "second failed\n\nExit code 1", results[1].Output)
	require.True(t, results[1].IsError)
}
