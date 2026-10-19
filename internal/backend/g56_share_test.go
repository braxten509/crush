package backend

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRemoteRestartUpdatesLaunch: a repeat start from the owner of a
// running share with a new launch ID answers on from the same share,
// starts no second one, and the share reports the new ID; an empty ID
// keeps the old one (G-26b).
func TestRemoteRestartUpdatesLaunch(t *testing.T) {
	t.Parallel()
	b, ws := shareTestWorkspace(t, nil)
	app, _ := shareTestApp(t, b, ws)

	srv := newShareTestServer()
	var starts atomic.Int32
	SetShareStarterForTest(ws, func(_ context.Context, _ *Workspace, launchID string) (ShareServer, error) {
		starts.Add(1)
		srv.SetLaunch(launchID)
		return srv, nil
	})

	first, err := b.StartRemote(t.Context(), ws.ID, app, "launch-1")
	require.NoError(t, err)
	require.Equal(t, remoteStateOn, first.State)
	require.Equal(t, "launch-1", srv.reportedLaunch())

	again, err := b.StartRemote(t.Context(), ws.ID, app, "launch-2")
	require.NoError(t, err)
	require.Equal(t, first, again, "the answer is the running share's status")
	require.Equal(t, int32(1), starts.Load(), "no second share starts")
	require.Equal(t, "launch-2", srv.reportedLaunch(), "the share reports the new launch")

	_, err = b.StartRemote(t.Context(), ws.ID, app, "")
	require.NoError(t, err)
	require.Equal(t, "launch-2", srv.reportedLaunch(), "an empty launch ID changes nothing")
	require.Equal(t, int32(1), starts.Load())
}
