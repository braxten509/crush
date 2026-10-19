package server

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestMainAgentSwitchPublishesAndNamesAgent: a main-agent switch sends
// config_changed to both apps on the project and GET agent names the new
// main agent; a switch refused as busy sends nothing (G-26).
func TestMainAgentSwitchPublishesAndNamesAgent(t *testing.T) {
	h := newE2EHarness(t)
	coord := &busySwitchCoordinator{runCoordinator: newRunCoordinator(func(context.Context) error { return nil })}
	h.app.AgentCoordinator = coord
	h.workspace.Cfg = config.NewTestStore(&config.Config{})

	wsPath := "/v1/workspaces/" + h.workspace.ID
	one := h.subscribeLiveSSE(t, h.workspace.ID, uuid.NewString())
	two := h.subscribeLiveSSE(t, h.workspace.ID, uuid.NewString())
	h.waitForAttached(t, 2)

	before := liveJSON[proto.AgentInfo](t, h, http.MethodGet, wsPath+"/agent", nil, http.StatusOK)
	require.Equal(t, "coder", before.Agent)

	liveJSON[struct{}](t, h, http.MethodPost, wsPath+"/agent/main", proto.AgentSetMainRequest{AgentID: "plan"}, http.StatusOK)
	for _, events := range []<-chan pubsub.Payload{one, two} {
		ev, ok := nextLive[proto.ConfigChanged](t, events, pubsub.PayloadTypeConfigChanged, 5*time.Second)
		require.True(t, ok, "every app on the project gets config_changed")
		require.Equal(t, h.workspace.ID, ev.Payload.WorkspaceID)
	}
	after := liveJSON[proto.AgentInfo](t, h, http.MethodGet, wsPath+"/agent", nil, http.StatusOK)
	require.Equal(t, "plan", after.Agent)

	coord.busyNow.Store(true)
	liveError(t, h, http.MethodPost, wsPath+"/agent/main", proto.AgentSetMainRequest{AgentID: "coder"}, http.StatusConflict, "agent_busy", "")
	_, got := nextLive[proto.ConfigChanged](t, one, pubsub.PayloadTypeConfigChanged, 500*time.Millisecond)
	require.False(t, got, "a refused switch publishes nothing")
}

// busySwitchCoordinator is a runCoordinator whose busy state can change
// while the server reads it.
type busySwitchCoordinator struct {
	*runCoordinator
	busyNow atomic.Bool
}

func (c *busySwitchCoordinator) IsBusy() bool { return c.busyNow.Load() }
