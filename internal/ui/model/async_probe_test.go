package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAsyncProbeQueuesWhileInFlight(t *testing.T) {
	t.Parallel()

	var p asyncProbe[int]
	require.True(t, p.stale(time.Hour), "never checked")
	require.True(t, p.request())
	require.True(t, p.start())
	require.False(t, p.stale(time.Hour), "dispatch stamps the check time")
	require.False(t, p.start(), "one fetch at a time")

	require.False(t, p.request(), "a request during a fetch is queued")
	require.False(t, p.request())
	require.True(t, p.finish(), "the queued request is handed back once")
	require.False(t, p.inFlight)

	require.True(t, p.start())
	require.False(t, p.finish(), "nothing queued")
	require.True(t, p.request())
}
