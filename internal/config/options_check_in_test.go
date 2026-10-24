package config

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOptionsGetCheckInInterval(t *testing.T) {
	t.Parallel()

	require.Equal(t, DefaultCheckInInterval, (*Options)(nil).GetCheckInInterval())
	require.Equal(t, 2*time.Minute, (&Options{}).GetCheckInInterval())
	require.Equal(t, 5*time.Minute, (&Options{CheckInMinutes: ptr(5)}).GetCheckInInterval())
	require.Zero(t, (&Options{CheckInMinutes: ptr(0)}).GetCheckInInterval(), "0 turns check-ins off")
	require.Zero(t, (&Options{CheckInMinutes: ptr(-3)}).GetCheckInInterval())

	var opts Options
	require.NoError(t, json.Unmarshal([]byte(`{"check_in_minutes": 0}`), &opts))
	require.Zero(t, opts.GetCheckInInterval())
}

func TestOptionsGetInterruptAfterToolCalls(t *testing.T) {
	t.Parallel()

	require.Equal(t, DefaultInterruptAfterToolCalls, (*Options)(nil).GetInterruptAfterToolCalls())
	require.Equal(t, 21, (&Options{}).GetInterruptAfterToolCalls())
	require.Equal(t, 8, (&Options{InterruptAfterToolCalls: ptr(8)}).GetInterruptAfterToolCalls())
	require.Zero(t, (&Options{InterruptAfterToolCalls: ptr(0)}).GetInterruptAfterToolCalls(), "0 disables interrupts")
	require.Zero(t, (&Options{InterruptAfterToolCalls: ptr(-2)}).GetInterruptAfterToolCalls())

	var opts Options
	require.NoError(t, json.Unmarshal([]byte(`{"interrupt_after_tool_calls": 0}`), &opts))
	require.Zero(t, opts.GetInterruptAfterToolCalls())
}
