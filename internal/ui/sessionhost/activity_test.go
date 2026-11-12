package sessionhost

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"
)

func TestBackgroundWaitDoesNotLookFinished(t *testing.T) {
	h := newTestHost(t, 120, 30, Status{Title: "Shown"}, Status{Title: "Other"})
	other := h.sessions[1]
	for _, state := range []State{StateWorking, StateBackground, StateWorking, StateBackground} {
		other.status.State = state
		h.statusChanged(other.id)
		require.False(t, other.unread, "unfinished background work must not show the completion check")
		mark, _ := h.sessionState(other, h.palette())
		require.Contains(t, spinnerFrames[:], mark)
	}
	other.status.State = StateReady
	h.statusChanged(other.id)
	require.True(t, other.unread, "completion after a background wait still needs the unread mark")
	h.show(1)
	require.False(t, other.unread, "showing the session reads it")
}

func TestActivityClockRunsOnlyForVisibleWorkingSessions(t *testing.T) {
	h := newTestHost(t, 120, 9,
		Status{Title: "Ready", State: StateReady},
		Status{Title: "Working", State: StateWorking},
	)
	_, cmd := h.Update(statusMsg{id: 2})
	require.Nil(t, cmd, "an offscreen working dot does not need ticks")
	h.show(1)
	_, cmd = h.Update(statusMsg{id: 2})
	require.NotNil(t, cmd, "showing a working dot starts its clock")
	_, cmd = h.Update(outputMsg{id: 2})
	require.Nil(t, cmd, "terminal output must not start a second clock")

	h.Update(tea.BlurMsg{})
	_, cmd = h.Update(activityTickMsg{})
	require.NotNil(t, cmd, "unfocused windows can still be visible and must keep the shared rhythm")
	_, cmd = h.Update(tea.FocusMsg{})
	require.Nil(t, cmd, "focus must not start another clock")

	h.sessions[1].status.State = StateBackground
	h.Update(statusMsg{id: 2})
	_, cmd = h.Update(activityTickMsg{})
	require.NotNil(t, cmd, "background work keeps the same clock going")

	h.sessions[1].status.State = StateReady
	h.Update(statusMsg{id: 2})
	_, cmd = h.Update(activityTickMsg{})
	require.Nil(t, cmd, "finished work stops the clock")

	h.sessions[1].status.State = StateWorking
	h.Update(statusMsg{id: 2})
	h.sessions[1].stopped = true
	_, cmd = h.Update(activityTickMsg{})
	require.Nil(t, cmd, "a closing session must not keep animating")
}
