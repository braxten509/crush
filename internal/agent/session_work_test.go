package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTreeGuardIgnoresOtherChatsJobs(t *testing.T) {
	h, _, _ := savingJobHub(t)
	listTestHub(t, h)
	a, err := h.c.sessions.Create(t.Context(), "a")
	require.NoError(t, err)
	b, err := h.c.sessions.Create(t.Context(), "b")
	require.NoError(t, err)
	require.NoError(t, SessionBackgroundBlock(a.ID))

	startJob(t, h, b.ID, "sleep 30")
	require.NoError(t, SessionBackgroundBlock(a.ID), "a job in another chat does not block")
	require.EqualError(t, SessionBackgroundBlock(b.ID), "wait for background commands and their completion messages before switching branches")

	startJob(t, h, a.ID, "sleep 30")
	require.EqualError(t, SessionBackgroundBlock(a.ID), "wait for background commands and their completion messages before switching branches")
}
