package rpc

import (
	"net/http"
	"testing"

	"github.com/charmbracelet/crush/internal/proto"
	"github.com/stretchr/testify/require"
)

func TestOpURLFillsParamsInOrder(t *testing.T) {
	t.Parallel()
	require.Equal(t, "/v1/workspaces/ws/sessions/s1/messages/m1/review", MessageReview.URL("ws", "s1", "m1"))
	require.Equal(t, "/v1/workspaces", ListWorkspaces.URL())
	require.Equal(t, "/v1/workspaces/ws/sessions//history", SessionHistory.URL("ws"), "missing args leave the segment empty")
	require.Equal(t, "/v1/workspaces/ws", GetWorkspace.URL("ws", "extra"), "surplus args are ignored")
}

func TestOpBodiesAndStatus(t *testing.T) {
	t.Parallel()

	require.False(t, Health.HasRequestBody())
	require.False(t, Health.HasResponseBody())
	require.Nil(t, Health.RequestExample())
	require.Nil(t, Health.ResponseExample())
	require.Equal(t, http.StatusOK, Health.RouteStatus())

	require.True(t, SendMessage.HasRequestBody())
	require.False(t, SendMessage.HasResponseBody())
	require.Equal(t, proto.AgentMessage{}, SendMessage.RequestExample())
	require.Equal(t, http.StatusAccepted, SendMessage.RouteStatus())

	// A struct{} response is a real (empty object) body, unlike Empty.
	require.True(t, ChangeSavedSession.HasResponseBody())
	require.Equal(t, struct{}{}, ChangeSavedSession.ResponseExample())

	// Pointer and slice zero values still carry their type for schemas.
	require.IsType(t, (*proto.AgentMessage)(nil), RecallQueued.ResponseExample())
	require.IsType(t, []proto.Session(nil), ListSessions.ResponseExample())
}
