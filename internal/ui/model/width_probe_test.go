package model

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"
)

func TestWidthProbeAnswerIsHandledOnce(t *testing.T) {
	m := &UI{}
	require.Nil(t, m.handleWidthProbe(tea.CursorPositionMsg{X: 2, Y: 1}), "no probe sent")

	m.widthProbePending = true
	require.Nil(t, m.handleWidthProbe(tea.CursorPositionMsg{X: 5, Y: 9}), "unrelated report")
	require.True(t, m.widthProbePending)

	require.NotNil(t, m.handleWidthProbe(tea.CursorPositionMsg{X: 2, Y: 1}))
	require.False(t, m.widthProbePending)
	require.Nil(t, m.handleWidthProbe(tea.CursorPositionMsg{X: 2, Y: 1}), "answered already")
}

func TestNarrowWidthProbeOnlyRepaints(t *testing.T) {
	m := &UI{widthProbePending: true}
	cmd := m.handleWidthProbe(tea.CursorPositionMsg{X: 1, Y: 1})
	require.NotNil(t, cmd)
	require.Equal(t, tea.ClearScreen(), cmd())
}

func TestWideWidthProbeKeepsReportedGraphemeSupport(t *testing.T) {
	m := &UI{widthProbePending: true}
	m.caps.UnicodeCore = true
	cmd := m.handleWidthProbe(tea.CursorPositionMsg{X: 2, Y: 1})
	require.NotNil(t, cmd)
	require.Equal(t, tea.ClearScreen(), cmd(), "renderer already measures graphemes")
}
