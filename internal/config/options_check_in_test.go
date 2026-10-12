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
