package model

import (
	"image/color"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"
)

var konsoleDefault = color.RGBA{0x23, 0x26, 0x27, 0xff}

func TestMatchingBackgroundRepliesNeverReachTheModel(t *testing.T) {
	t.Parallel()

	u := newTestUI()
	f := NewFilter()
	require.Nil(t, f.Filter(u, tea.BackgroundColorMsg{Color: u.com.Styles.Background}), "a check that finds Crush's color must not redraw")
	require.NotNil(t, f.Filter(u, tea.BackgroundColorMsg{Color: konsoleDefault}), "a lost color must reach the model")
}

func TestDroppedTerminalBackgroundIsSentAgain(t *testing.T) {
	t.Parallel()

	u := newTestUI()
	want := u.com.Styles.Background
	first := u.viewBackground()

	u.handleTerminalBackground(tea.BackgroundColorMsg{Color: konsoleDefault})
	again := u.viewBackground()
	require.NotEqual(t, first, again, "a changed value makes the renderer send the color again")
	require.True(t, sameRGB(want, again), "it is still Crush's color")

	// Taken: the count resets, so a later loss is fixed again.
	u.handleTerminalBackground(tea.BackgroundColorMsg{Color: want})
	require.Zero(t, u.backgroundResends)

	// A terminal that never takes the color isn't repainted forever.
	for range 10 {
		u.handleTerminalBackground(tea.BackgroundColorMsg{Color: konsoleDefault})
	}
	require.Equal(t, maxBackgroundResends, u.backgroundResends)
	require.Nil(t, NewFilter().Filter(u, tea.BackgroundColorMsg{Color: konsoleDefault}))
}

func TestTerminalBackgroundIsLeftAloneWhenTransparent(t *testing.T) {
	t.Parallel()

	u := newTestUI()
	u.isTransparent = true
	require.Nil(t, NewFilter().Filter(u, tea.BackgroundColorMsg{Color: konsoleDefault}))
	u.handleTerminalBackground(tea.BackgroundColorMsg{Color: konsoleDefault})
	require.Zero(t, u.backgroundResends)
}

func TestBackgroundChecksReuseTheLastFrame(t *testing.T) {
	t.Parallel()

	u := newTestUI()
	u.lastView, u.haveLastView = tea.View{AltScreen: true, WindowTitle: "last frame"}, true
	query := tea.RequestBackgroundColor()
	require.True(t, isBackgroundQuery(query))
	require.Equal(t, query, NewFilter().Filter(u, query), "the query still goes to the terminal")
	require.Equal(t, "last frame", u.View().WindowTitle, "but nothing is redrawn")
	require.False(t, u.quietFrame)
	require.False(t, isBackgroundQuery(tea.KeyPressMsg{}))
}
