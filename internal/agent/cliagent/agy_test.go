package cliagent

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/jsonfile"
	"github.com/stretchr/testify/require"
)

// Once AGY has recovered from a failed step it marks every later turn of
// the conversation as failed with that step's error. A turn that ended on a
// finished reply is not a failure; one that stopped anywhere else still is.
func TestAGYFinishedReplyIsNotAFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // skip the real ~/.local/bin/agy
	fakeCLI(t, "agy", `{"event":"init","conversation_id":"conv1"}
{"event":"step_update","step_update":{"step_index":7,"state":"ACTIVE","step_type":"agent_response","text_delta":"OK."}}
{"event":"step_update","step_update":{"step_index":7,"state":"DONE","step_type":"agent_response"}}
{"event":"result","result":{"status":"ERROR","response":"OK.","error":"an error from an earlier step","finished_reason":"error"}}
`)
	events, err := collect(t, "agy", "")
	require.NoError(t, err)
	require.Contains(t, types(events), EventText)

	fakeCLI(t, "agy", `{"event":"init","conversation_id":"conv1"}
{"event":"step_update","step_update":{"step_index":7,"state":"DONE","step_type":"agent_response","text_delta":"Looking."}}
{"event":"step_update","step_update":{"step_index":8,"state":"DONE","step_type":"tool","tool_name":"run_command","tool_info":{"parameters":{"CommandLine":"ls"}}}}
{"event":"result","result":{"status":"ERROR","response":"Looking.","error":"quota reached","finished_reason":"error"}}
`)
	_, err = collect(t, "agy", "")
	require.ErrorContains(t, err, "quota reached")
}

// The model's context window is asked from AGY once, saved, and reaches the
// context meter as the chat's limit.
func TestAGYContextWindowReachesTheMeter(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // skip the real ~/.local/bin/agy
	t.Setenv("CRUSH_CACHE_DIR", t.TempDir())
	var asks atomic.Int32
	agyAskWindow = func(context.Context, string) (int64, error) {
		asks.Add(1)
		return 1_048_576, nil
	}
	t.Cleanup(func() { agyAskWindow = nil })
	fakeCLI(t, "agy", `{"event":"init","conversation_id":"conv1"}
{"event":"step_update","step_update":{"step_index":1,"state":"DONE","step_type":"agent_response","text_delta":"Hi."}}
{"event":"result","result":{"status":"SUCCESS"}}
`)
	_, err := collect(t, "agy", "")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return jsonfile.Read[map[string]agyWindow](agyWindowFile())["m"].Size > 0
	}, 5*time.Second, 10*time.Millisecond)

	events, err := collect(t, "agy", "")
	require.NoError(t, err)
	var limits []Event
	for _, e := range events {
		if e.Type == EventContextBudget {
			limits = append(limits, e)
		}
	}
	require.Len(t, limits, 1)
	require.EqualValues(t, 1_048_576, limits[0].ContextLimit)
	require.True(t, limits[0].ContextEstimated)
	require.EqualValues(t, 1, asks.Load(), "a saved window is not asked for again")
}
