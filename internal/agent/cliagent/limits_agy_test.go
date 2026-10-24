package cliagent

import (
	"context"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

// AGY's plan is read over HTTP, so nothing in its stream carries new
// usage: the meter must be refreshed as its tool steps run, not only when
// the turn ends.
func TestAGYLimitsRefreshDuringToolSteps(t *testing.T) {
	fakeCLI(t, "agy", `{"event":"init","conversation_id":"conv1"}
{"event":"step_update","step_update":{"step_index":1,"state":"ACTIVE","step_type":"tool","tool_name":"run_command","tool_info":{"parameters":{"CommandLine":"touch a"}}}}
{"event":"step_update","step_update":{"step_index":1,"state":"DONE","step_type":"tool","tool_name":"run_command","tool_info":{"parameters":{"CommandLine":"touch a"}}}}
{"event":"result","result":{"status":"SUCCESS"}}
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
