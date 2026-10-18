package cmd

import (
	"path/filepath"
	"testing"

	"github.com/charmbracelet/crush/internal/memlimit"
	"github.com/stretchr/testify/require"
)

// TestServerJoinsMemoryBox: `crush server` and `crush engine serve` both
// join the memory box before anything else. Each run here stops at its
// first check, which comes after the join.
func TestServerJoinsMemoryBox(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))

	var joined []string
	oldJoin := joinMemoryBox
	joinMemoryBox = func(slice string) error {
		joined = append(joined, slice)
		return nil
	}
	t.Cleanup(func() { joinMemoryBox = oldJoin })

	// No private socket folder leaves --host empty.
	oldHost := serverHost
	serverHost = ""
	t.Cleanup(func() { serverHost = oldHost })
	require.ErrorContains(t, serverCmd.RunE(serverCmd, nil), "no server host")
	require.Equal(t, []string{memlimit.Slice}, joined, "crush server joins first")

	require.EqualError(t, engineServeCmd.RunE(engineServeCmd, nil), "--data-dir is required")
	require.Equal(t, []string{memlimit.Slice, memlimit.Slice}, joined, "crush engine serve joins first")
}
