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

	hold, _ := gate(t)
	startJob(t, h, b.ID, hold)
	require.NoError(t, SessionBackgroundBlock(a.ID), "a job in another chat does not block")
	require.Error(t, SessionBackgroundBlock(b.ID))

	startJob(t, h, a.ID, hold)
	require.Error(t, SessionBackgroundBlock(a.ID))
}
