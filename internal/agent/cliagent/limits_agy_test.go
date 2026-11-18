package cliagent

import (
	"context"
	"testing"
	"time"

	"charm.land/catwalk/v3/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

// AGY's plan is read over HTTP, so nothing in its ACP stream carries new
// usage: the meter must be refreshed as its tool steps run, not only when
// the turn ends.
func TestAGYLimitsRefreshDuringToolSteps(t *testing.T) {
	fakeCLI(t, "agy", `{"jsonrpc":"2.0","id":"1","result":{"protocolVersion":1}}
{"jsonrpc":"2.0","id":"2","result":{"sessionId":"conv1"}}
{"jsonrpc":"2.0","id":"set-model","result":{}}
{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"tool_call","toolCallId":"c1","name":"run_command","kind":"execute","title":"touch a","status":"in_progress","rawInput":{"CommandLine":"touch a"}}}}
{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"tool_call_update","toolCallId":"c1","name":"run_command","kind":"execute","status":"completed","rawInput":{"CommandLine":"touch a"}}}}
{"jsonrpc":"2.0","id":"3","result":{"stopReason":"end_turn"}}
`)
	// Skip the real ~/.local/bin/agy.
	t.Setenv("HOME", t.TempDir())
	fetched := make(chan struct{}, 1)
	restore := SetLimitFetchersForTest(map[catwalk.Type]func(context.Context) ([]Limit, error){
		config.TypeAGYCLI: func(context.Context) ([]Limit, error) {
			select {
			case fetched <- struct{}{}:
			default:
			}
			return []Limit{{Name: "5h", Used: 42}}, nil
		},
	})
	defer restore()

	_, err := collect(t, "agy", "")
	require.NoError(t, err)
	select {
	case <-fetched:
	case <-time.After(time.Second):
		t.Fatal("AGY tool step did not refresh usage limits")
	}
	require.Eventually(t, func() bool {
		limits := Limits(config.TypeAGYCLI, "")
		return len(limits) == 1 && limits[0].Used == 42
	}, time.Second, 10*time.Millisecond)
}
