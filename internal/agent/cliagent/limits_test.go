package cliagent

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLimitParsing(t *testing.T) {
	t.Parallel()

	claude := claudeRateLimit([]byte(`{"type":"rate_limit_event","rate_limit_info":{"unifiedWindows":{"five_hour":{"utilization":0.16,"resetsAt":1790421000},"seven_day":{"utilization":0.81,"resetsAt":1790470800}}}}`))
	require.Len(t, claude, 2)
	require.Equal(t, "5h", claude[0].Name)
	require.InDelta(t, 16, claude[0].Used, 0.001)
	require.Equal(t, "Weekly", claude[1].Name)
	require.Equal(t, time.Unix(1790470800, 0), claude[1].ResetsAt)

	// Resets in the future, or Left() reports the window as full again.
	resets := time.Now().Add(time.Hour).Unix()
	var snap codexSnapshot
	require.NoError(t, json.Unmarshal(fmt.Appendf(nil, `{"limitId":"codex","primary":{"usedPercent":73,"windowDurationMins":10080,"resetsAt":%d},"secondary":{"usedPercent":5,"windowDurationMins":300,"resetsAt":0}}`, resets), &snap))
	codex := snap.limits()
	require.Equal(t, []Limit{{Name: "Weekly", Used: 73, ResetsAt: time.Unix(resets, 0)}, {Name: "5h", Used: 5}}, codex)
	require.InDelta(t, 27, codex[0].Left(), 0.001)

	other := "codex_other"
	snap.LimitID = &other
	require.Nil(t, snap.limits(), "other quota buckets are ignored")

	past := Limit{Used: 90, ResetsAt: time.Now().Add(-time.Minute)}
	require.InDelta(t, 100, past.Left(), 0.001, "a window that reset is full again")
}

func TestGrokBilling(t *testing.T) {
	t.Parallel()

	limits, err := grokBilling([]byte(`{"config":{"currentPeriod":{"type":"USAGE_PERIOD_TYPE_WEEKLY","end":"2026-10-01T23:03:33.462849+00:00"},"creditUsagePercent":{"val":42}}}`))
	require.NoError(t, err)
	require.Len(t, limits, 1)
	require.Equal(t, "Weekly", limits[0].Name)
	require.InDelta(t, 42, limits[0].Used, 0.001)

	limits, err = grokBilling([]byte(`{"config":{"currentPeriod":{"type":"USAGE_PERIOD_TYPE_WEEKLY","end":"2026-10-01T23:03:33Z"}}}`))
	require.NoError(t, err)
	require.InDelta(t, 100, limits[0].Left(), 0.001, "nothing used yet")
}
