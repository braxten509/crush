//go:build !windows

package cliagent

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestKillCommands(t *testing.T) {
	if _, err := os.Stat("/proc/self/stat"); err != nil {
		t.Skip("needs /proc")
	}
	// A "CLI" whose command runs in a session of its own, like AGY's.
	p, err := startProc(t.TempDir(), "sh", "-c", "setsid sleep 30 & echo started; wait")
	require.NoError(t, err)
	require.True(t, p.lines.Scan())
	var child int
	require.Eventually(t, func() bool {
		out, _ := exec.Command("pgrep", "-P", strconv.Itoa(p.cmd.Process.Pid)).Output()
		child, _ = strconv.Atoi(strings.TrimSpace(string(out)))
		return child > 0
	}, 5*time.Second, 20*time.Millisecond)

	p.killCommands()
	require.Eventually(t, func() bool {
		return syscall.Kill(child, 0) != nil
	}, 5*time.Second, 20*time.Millisecond)
	p.finish()
}
