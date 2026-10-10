package cliagent

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNativeClaudeContextReport(t *testing.T) {
	fakeCLI(t, "claude", `{"type":"system","subtype":"init","session_id":"s1","claude_code_version":"2.1.288"}
{"type":"control_response","response":{"request_id":"crush-context","subtype":"success","response":{"autoCompactThreshold":467000,"maxTokens":500000,"rawMaxTokens":1000000,"isAutoCompactEnabled":true}}}
{"type":"result","subtype":"success"}
`)
	events, err := collect(t, "claude", "")
	require.NoError(t, err)
	var budgets []Event
	for _, event := range events {
		if event.Type == EventContextBudget {
			budgets = append(budgets, event)
		}
	}
	require.Len(t, budgets, 1)
	require.EqualValues(t, 467000, budgets[0].ContextLimit)
	require.False(t, budgets[0].ContextEstimated)
	_, ok := claudeContextBudget(json.RawMessage(`{"request_id":"crush-context","subtype":"error","error":"unsupported"}`))
	require.False(t, ok)
}

func TestCodexContextRespectsNativeSettingsAndWindow(t *testing.T) {
	for _, test := range []struct {
		name                                   string
		settings                               codexContextSettings
		window, limit, reported, percent, want int64
	}{
		{"catalog default", codexContextSettings{}, 1000000, 0, 0, 95, 900000},
		{"user override", codexContextSettings{Limit: 300000}, 1000000, 800000, 0, 95, 300000},
		{"native headroom cap", codexContextSettings{Limit: 500000}, 400000, 0, 0, 95, 360000},
		{"configured window", codexContextSettings{Window: 200000}, 1000000, 0, 0, 95, 180000},
		{"reported window", codexContextSettings{}, 1000000, 0, 380000, 95, 360000},
		{"unknown", codexContextSettings{}, 0, 0, 0, 0, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := codexContextBudget(test.settings, test.window, test.limit, test.reported, test.percent)
			require.Equal(t, test.want, result.ContextLimit)
			require.True(t, result.ContextEstimated)
		})
	}
}

func TestOpenCodeContextBudgetKeepsOutputReserve(t *testing.T) {
	result := openCodeContextBudget(1_000_000)
	require.Equal(t, EventContextBudget, result.Type)
	require.EqualValues(t, 968_000, result.ContextLimit)
	require.True(t, result.ContextEstimated)
	require.Zero(t, openCodeContextBudget(10_000).ContextLimit)
}
