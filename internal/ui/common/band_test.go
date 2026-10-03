package common

import (
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestOnBandKeepsBackgroundAfterResets(t *testing.T) {
	bg := lipgloss.Color("#282a2e")
	on := ansi.NewStyle().BackgroundColor(bg).String()
	line := lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Render("hi") + " there"

	out := OnBand(line, 12, bg)

	require.Equal(t, 12, ansi.StringWidth(out))
	require.Equal(t, "hi there    ", ansi.Strip(out))
	// Every reset inside the line is followed by the band again.
	require.NotContains(t, out[:len(out)-len("\x1b[m")], "\x1b[m ")
	require.Contains(t, out, "\x1b[m"+on)
}

func TestClearsBackground(t *testing.T) {
	require.True(t, clearsBackground(""))
	require.True(t, clearsBackground("0"))
	require.True(t, clearsBackground("1;49"))
	require.False(t, clearsBackground("38;5;0"))
	require.False(t, clearsBackground("48;2;10;0;0"))
	require.False(t, clearsBackground("38:2::0:0:0"))
	require.True(t, clearsBackground("38;2;1;2;3;0"))
}
