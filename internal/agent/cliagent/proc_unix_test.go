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
	// A "CLI" whose commands run in sessions of their own, like Codex's:
	// one left in the background and two the turn waits on (one through a
	// shell, one run directly), started together.
	p, err := startProc(t.TempDir(), "sh", "-c", "setsid sh -c 'sleep 30; :' & setsid sh -c 'sleep 31; :' & setsid sleep 32 & echo started; wait")
	require.NoError(t, err)
	at := time.Now()
	require.True(t, p.lines.Scan())
	child := func(cmd string) (pid int) {
		require.Eventually(t, func() bool {
			out, _ := exec.Command("pgrep", "-P", strconv.Itoa(p.cmd.Process.Pid), "-fx", cmd).Output()
			pid, _ = strconv.Atoi(strings.TrimSpace(string(out)))
			return pid > 0
		}, 5*time.Second, 20*time.Millisecond)
		return pid
	}
	background, shell, direct := child("sh -c sleep 30; :"), child("sh -c sleep 31; :"), child("sleep 32")

	p.killCommands([]openCall{{command: "sleep 31", at: at}, {command: "'sleep' 32", words: []string{"sleep", "32"}, at: at}})
	require.Eventually(t, func() bool {
		return syscall.Kill(shell, 0) != nil && syscall.Kill(direct, 0) != nil
	}, 5*time.Second, 20*time.Millisecond)
	require.NoError(t, syscall.Kill(background, 0), "the background command was killed")
	_ = syscall.Kill(-background, syscall.SIGKILL)
	p.kill()
	p.finish()
}
