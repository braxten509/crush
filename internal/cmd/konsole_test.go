package cmd

import (
	"bytes"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/stretchr/testify/require"
)

func TestHideKonsoleScrollMarker(t *testing.T) {
	t.Parallel()

	konsole := uv.Environ{"TERM=xterm-256color", "KONSOLE_VERSION=250800"}

	var out bytes.Buffer
	restore := hideKonsoleScrollMarker(konsole, &out, true)
	require.Equal(t, konsoleHideScrollMarker, out.String(), "Konsole gets the marker switched off at start")
	out.Reset()
	restore()
	require.Equal(t, konsoleShowScrollMarker, out.String(), "and back on at exit")

	for name, tt := range map[string]struct {
		env uv.Environ
		tty bool
	}{
		"other terminal": {uv.Environ{"TERM=xterm-256color"}, true},
		"not a terminal": {konsole, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			hideKonsoleScrollMarker(tt.env, &out, tt.tty)()
			require.Empty(t, out.String())
		})
	}
}
