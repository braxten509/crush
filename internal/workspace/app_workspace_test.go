package workspace

import (
	"testing"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/app"
	"github.com/charmbracelet/crush/internal/backend"
	"github.com/stretchr/testify/require"
)

// busyCoordinator reports a busy agent and records SetMainAgent calls.
// Methods it does not override panic through the nil embedded
// interface, so the test fails loudly if an unexpected call happens.
type busyCoordinator struct {
	agent.Coordinator
	busy   bool
	setTo  string
	called bool
}

func (c *busyCoordinator) IsBusy() bool { return c.busy }

func (c *busyCoordinator) SetMainAgent(name string) error {
	c.called = true
	c.setTo = name
	return nil
}

// TestAppWorkspaceAgentSetMainRejectedWhileBusy covers the drift fixed by
// sharing the operation with the server: the in-process workspace used to
// switch the main agent mid-run, stranding the run's queued prompts on the
// previous agent, while the server refused.
func TestAppWorkspaceAgentSetMainRejectedWhileBusy(t *testing.T) {
	t.Parallel()

	coord := &busyCoordinator{busy: true}
	ws := NewAppWorkspace(&app.App{AgentCoordinator: coord}, nil)
	require.ErrorIs(t, ws.AgentSetMain("plan"), backend.ErrAgentBusy)
	require.False(t, coord.called)

	coord.busy = false
	require.NoError(t, ws.AgentSetMain("plan"))
	require.Equal(t, "plan", coord.setTo)
}

func TestAppWorkspaceAgentSetMainWithoutAgent(t *testing.T) {
	t.Parallel()

	ws := NewAppWorkspace(&app.App{}, nil)
	err := ws.AgentSetMain("plan")
	require.ErrorIs(t, err, backend.ErrAgentNotInitialized)
	require.EqualError(t, err, "agent coordinator not initialized")
}
