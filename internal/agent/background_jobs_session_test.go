package agent

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestManagedJobsCarrySession(t *testing.T) {
	h, _, _ := savingJobHub(t)
	listTestHub(t, h)
	chat, err := h.c.sessions.Create(t.Context(), "jobs")
	require.NoError(t, err)
	other, err := h.c.sessions.Create(t.Context(), "other")
	require.NoError(t, err)
	hold, _ := gate(t)
	job := startJob(t, h, chat.ID, hold)

	isJob := func(p Process) bool { return p.JobID == job.ID }
	all := BackgroundProcesses()
	i := slices.IndexFunc(all, isJob)
	require.GreaterOrEqual(t, i, 0, "the managed job is listed")
	require.Equal(t, chat.ID, all[i].Session, "with the chat that started it")
	require.True(t, slices.ContainsFunc(SessionBackgroundProcesses(chat.ID), isJob))
	require.False(t, slices.ContainsFunc(SessionBackgroundProcesses(other.ID), isJob))
}
