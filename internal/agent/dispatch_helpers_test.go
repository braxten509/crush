package agent

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestChatHelpersListAndStopAPIHelpers: an API chat's running subagent is in
// that chat's helper list (and no other chat's), and the user's stop on it
// stops it. Before, the list only held `crush spawn` helpers.
func TestChatHelpersListAndStopAPIHelpers(t *testing.T) {
	c, _ := newDispatchTestCoordinator(t)
	registerTestHub(t).c = c
	entry := startBlockingDispatch(t, c, "parent", "scout")
	t.Cleanup(func() { entry.stop("test over"); entry.awaitSettled(5 * time.Second) })

	find := func(sessionID string) (Task, bool) {
		for _, h := range ChatHelpers(sessionID) {
			if h.API && h.Name == "scout" {
				return h, true
			}
		}
		return Task{}, false
	}

	got, ok := find("parent")
	require.True(t, ok, "the running API helper is listed")
	require.Equal(t, TaskRunning, got.Status)
	require.Equal(t, entry.SessionID, got.ChildID)
	_, ok = find("another-chat")
	require.False(t, ok, "another chat does not list it")

	require.NoError(t, StopChatHelper("parent", got.ID))
	require.True(t, entry.awaitSettled(5*time.Second), "the stop ends its run")
	got, ok = find("parent")
	require.True(t, ok)
	require.Equal(t, TaskStopped, got.Status)
}
