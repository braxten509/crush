package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestThoughtLineIsOnUnlessTurnedOff(t *testing.T) {
	t.Parallel()
	off, on := false, true
	var none *TUIOptions
	require.True(t, none.ShowsThoughtLine())
	require.True(t, (&TUIOptions{}).ShowsThoughtLine())
	require.True(t, (&TUIOptions{ThoughtLine: &on}).ShowsThoughtLine())
	require.False(t, (&TUIOptions{ThoughtLine: &off}).ShowsThoughtLine())
}
