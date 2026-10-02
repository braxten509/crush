package common

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestWidthProbeIsAnEmojiSequenceWcwidthUndercounts(t *testing.T) {
	// The probe only tells the methods apart if they disagree on it.
	require.Equal(t, 1, ansi.StringWidthWc(widthProbeText))
	require.Equal(t, 2, ansi.StringWidth(widthProbeText))
	probe := WidthProbe()
	require.Contains(t, probe, ansi.CursorPosition(1, widthProbeRow)+widthProbeText+ansi.RequestCursorPositionReport)
	require.Equal(t, ansi.SaveCursor, probe[:len(ansi.SaveCursor)])
	require.Equal(t, ansi.RestoreCursor, probe[len(probe)-len(ansi.RestoreCursor):])
}

func TestWidthProbeResult(t *testing.T) {
	for _, tc := range []struct {
		msg      tea.CursorPositionMsg
		wide, ok bool
	}{
		{tea.CursorPositionMsg{X: 2, Y: widthProbeRow - 1}, true, true},
		{tea.CursorPositionMsg{X: 1, Y: widthProbeRow - 1}, false, true},
		{tea.CursorPositionMsg{X: 2, Y: 0}, false, false}, // another report
		{tea.CursorPositionMsg{X: 7, Y: widthProbeRow - 1}, false, false},
	} {
		wide, ok := WidthProbeResult(tc.msg)
		require.Equal(t, tc.wide, wide, "%+v", tc.msg)
		require.Equal(t, tc.ok, ok, "%+v", tc.msg)
	}
}

func TestCapabilitiesTrackUnicodeCore(t *testing.T) {
	var caps Capabilities
	caps.Update(tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: ansi.ModeNotRecognized})
	require.False(t, caps.UnicodeCore)
	caps.Update(tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: ansi.ModeReset})
	require.True(t, caps.UnicodeCore)
}

func TestWidthMethodFollowsUnicodeCore(t *testing.T) {
	var caps Capabilities
	require.Equal(t, ansi.WcWidth, caps.WidthMethod())
	caps.Update(tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: ansi.ModeSet})
	require.Equal(t, ansi.GraphemeWidth, caps.WidthMethod())
}
