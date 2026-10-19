package backend

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAuthFlowClaimRefusesSavingOnceContextDone: a running flow whose
// context is already cancelled can't move to saving, even though nothing
// else claimed it; it can still end, so its failure is reported. A flow
// with a live context saves (G-5).
func TestAuthFlowClaimRefusesSavingOnceContextDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := &authFlow{ctx: ctx, cancel: cancel}
	require.False(t, done.claim(authFlowSavingState), "a done context never saves")
	require.Equal(t, authFlowRunning, done.state, "the refused claim changes nothing")
	require.True(t, done.claim(authFlowOver), "the flow can still end")

	live, stop := context.WithCancel(context.Background())
	defer stop()
	running := &authFlow{ctx: live, cancel: stop}
	require.True(t, running.claim(authFlowSavingState), "a live flow saves")
	require.Equal(t, authFlowSavingState, running.state)
}
