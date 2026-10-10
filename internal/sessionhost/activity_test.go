package sessionhost

import (
	"image/color"
	"testing"
	"time"

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
		mark, c := h.sessionState(other, h.palette())
		require.Equal(t, "●", mark)
		if state == StateBackground {
			require.Equal(t, h.palette().muted, c)
		}
	}
	other.status.State = StateReady
	h.statusChanged(other.id)
	require.True(t, other.unread, "completion after a background wait still needs the unread mark")
	require.Contains(t, screenText(h), "✓ Other")
	h.show(1)
	require.Contains(t, screenText(h), "○ Other")
}

func TestActivityPulseKeepsDotsAndTextInPlace(t *testing.T) {
	for _, width := range []int{120, 60} {
		h := newTestHost(t, width, 30,
			Status{Title: "Working", State: StateWorking},
			Status{Title: "Background", State: StateBackground},
			Status{Title: "Ready", State: StateReady},
		)
		h.applyTheme("graphite")
		text := screenText(h)
		colors := map[color.RGBA]bool{}
		backgroundColors := map[color.RGBA]bool{}
		_, firstColor := h.sessionState(h.sessions[0], h.palette())
		for frame := 1; frame <= activityFrames; frame++ {
			_, cmd := h.Update(activityTickMsg{time.Unix(0, int64(frame)*int64(activityInterval))})
			require.NotNil(t, cmd)
			mark, c := h.sessionState(h.sessions[0], h.palette())
			require.Equal(t, "●", mark)
			colors[color.RGBAModel.Convert(c).(color.RGBA)] = true
			mark, c = h.sessionState(h.sessions[1], h.palette())
			require.Equal(t, "●", mark)
			backgroundColors[color.RGBAModel.Convert(c).(color.RGBA)] = true
			require.Equal(t, text, screenText(h), "only the activity dot colors may change")
		}
		require.Greater(t, len(colors), 2, "working must visibly pulse, not just blink on and off")
		require.Greater(t, len(backgroundColors), 2, "background work must pulse too")
		_, lastColor := h.sessionState(h.sessions[0], h.palette())
		require.Equal(t, firstColor, lastColor, "the pulse loops without a jump")
	}
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

func TestActivityDotsJoinTheSamePulse(t *testing.T) {
	h := newTestHost(t, 120, 30,
		Status{State: StateWorking},
		Status{State: StateReady},
	)
	h.applyTheme("graphite")
	for frame := 1; frame <= activityFrames/2; frame++ {
		h.Update(activityTickMsg{time.Unix(0, int64(frame)*int64(activityInterval))})
	}
	h.sessions[1].status.State = StateBackground
	h.Update(statusMsg{id: 2})
	_, joinedColor := h.sessionState(h.sessions[1], h.palette())
	require.NotEqual(t, h.palette().muted, joinedColor, "a later dot joins at the current dim phase")
	for frame := activityFrames/2 + 1; frame <= activityFrames; frame++ {
		h.Update(activityTickMsg{time.Unix(0, int64(frame)*int64(activityInterval))})
	}
	_, workingColor := h.sessionState(h.sessions[0], h.palette())
	_, backgroundColor := h.sessionState(h.sessions[1], h.palette())
	require.Equal(t, h.palette().working, workingColor)
	require.Equal(t, h.palette().muted, backgroundColor, "both reach full brightness on the same frame")
}

func TestActivityClockMatchesAcrossWindows(t *testing.T) {
	first := newTestHost(t, 120, 30, Status{State: StateWorking})
	second := newTestHost(t, 120, 30, Status{State: StateBackground})
	first.tickActivityAt(time.Unix(100, 0))
	later := time.Unix(103, 400_000_000)
	first.Update(activityTickMsg{later})
	second.tickActivityAt(later)
	require.Equal(t, first.activityFrame, second.activityFrame, "a newly opened window joins the same rhythm")
	first.Update(tea.BlurMsg{})
	second.Update(tea.FocusMsg{})
	next := later.Add(activityInterval)
	first.Update(activityTickMsg{next})
	second.Update(activityTickMsg{next})
	require.Equal(t, first.activityFrame, second.activityFrame, "moving focus between windows cannot break the rhythm")
}
