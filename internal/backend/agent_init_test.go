package backend

import (
	"context"
	"testing"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/stretchr/testify/require"
)

// TestNonInteractiveInitKeepsInteractiveCoordinator: a non-interactive
// InitAgent on a workspace whose interactive coordinator runs keeps that
// coordinator. A desktop turn with a helper, and a prompt queued behind it,
// stay on it: the turn runs on, the queued prompt runs next, and a new
// helper lands in the same helper hub.
func TestNonInteractiveInitKeepsInteractiveCoordinator(t *testing.T) {
	r := newLiveRig(t)
	a := newClientID(t)
	ws := r.open(t, r.project(t), a)
	c, d := r.chat(t, ws, "C"), r.chat(t, ws, "D")
	helper := r.spawnHelper(t, ws, a, c, "a1")
	r.send(t, ws, a, c, "a2", "live:hold:a2")
	requireQueued(t, ws, c, "a2")
	before := ws.coordinator()
	turnCalls, helperCalls := r.model.callsOf("a1"), r.model.callsOf(helper.Name)

	require.NoError(t, r.b.InitAgent(context.Background(), ws.ID, "", false))

	require.True(t, ws.coordinator() == before, "the interactive coordinator stays")
	require.True(t, ws.coordinator().IsSessionBusy(c), "the turn runs on it")
	require.Equal(t, 1, r.model.heldNow("a1"))
	require.Equal(t, turnCalls, r.model.callsOf("a1"), "and never restarts")
	requireQueued(t, ws, c, "a2")
	task, ok := sessionTask(c, helper.Name)
	require.True(t, ok)
	require.Equal(t, agent.TaskRunning, task.Status)
	require.Equal(t, 1, r.model.heldNow(helper.Name))
	require.Equal(t, helperCalls, r.model.callsOf(helper.Name))

	// The same helper hub: a new helper takes the hub's next task ID, not a
	// second hub's first one.
	next := r.spawnHelper(t, ws, a, d, "d1")
	require.NotEqual(t, helper.ID, next.ID)

	// And the queue drains on it.
	require.NoError(t, r.b.Interrupt(ws.ID, c))
	r.model.waitHeld(t, "a2", 1)
}
