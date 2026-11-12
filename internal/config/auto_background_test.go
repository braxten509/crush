package config

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Nothing saved gives the default, a saved time is used, and a saved time
// outside the accepted range is brought to its nearest end, so a
// hand-edited config can never turn the move off or make it near-instant.
func TestAutoBackgroundTime(t *testing.T) {
	seconds := func(n int) time.Duration { return time.Duration(n) * time.Second }
	var none *Options
	require.Equal(t, seconds(DefaultAutoBackgroundSeconds), none.GetAutoBackground())
	require.Equal(t, seconds(DefaultAutoBackgroundSeconds), (&Options{}).GetAutoBackground())

	var saved Options
	require.NoError(t, json.Unmarshal([]byte(`{"auto_background_seconds": 300}`), &saved))
	require.Equal(t, 300*time.Second, saved.GetAutoBackground())

	for _, low := range []int{-5, 0, MinAutoBackgroundSeconds - 1} {
		require.Equal(t, seconds(MinAutoBackgroundSeconds), (&Options{AutoBackgroundSeconds: &low}).GetAutoBackground())
		require.Error(t, ValidateAutoBackgroundSeconds(low))
	}
	high := MaxAutoBackgroundSeconds + 1
	require.Equal(t, seconds(MaxAutoBackgroundSeconds), (&Options{AutoBackgroundSeconds: &high}).GetAutoBackground())
	require.Error(t, ValidateAutoBackgroundSeconds(high))
	require.NoError(t, ValidateAutoBackgroundSeconds(MinAutoBackgroundSeconds))
	require.NoError(t, ValidateAutoBackgroundSeconds(MaxAutoBackgroundSeconds))
}
