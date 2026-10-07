//go:build !windows

package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

const crashGuardChildEnv = "CRUSH_CRASH_GUARD_TEST_CHILD"

// TestCrashGuardChild is the child the guard runs in the tests below.
func TestCrashGuardChild(t *testing.T) {
	mode := os.Getenv(crashGuardChildEnv)
	if mode == "" {
		t.Skip("only runs as the crash guard's child")
	}
	_, done := recordCrashes()
	switch mode {
	case "panic":
		go panic("boom from a background goroutine")
		select {}
	case "kill":
		_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
		select {}
	case "exit":
		done()
		os.Exit(3)
	}
}

func runGuardedChild(t *testing.T, mode string) (code int, report string, out, errOut *bytes.Buffer, restored bool) {
	t.Helper()
	report = filepath.Join(t.TempDir(), "crash.log")
	child := exec.Command(os.Args[0], "-test.run=^TestCrashGuardChild$")
	child.Env = append(os.Environ(), crashGuardChildEnv+"="+mode, crashReportEnv+"="+report, "XDG_STATE_HOME="+t.TempDir())
	out, errOut = &bytes.Buffer{}, &bytes.Buffer{}
	code, started := superviseChild(child, report, func() { restored = true }, out, errOut)
	require.True(t, started)
	return code, report, out, errOut, restored
}

func TestCrashGuardRestoresTerminalAfterPanic(t *testing.T) {
	code, report, out, errOut, restored := runGuardedChild(t, "panic")
	require.Equal(t, 2, code)
	require.True(t, restored)
	require.Contains(t, out.String(), terminalReset)
	require.Equal(t, "Crush crashed. Details saved to "+report+"\n", errOut.String())
	saved, err := os.ReadFile(report)
	require.NoError(t, err)
	require.Contains(t, string(saved), "boom from a background goroutine")
}

func TestCrashGuardReportsKill(t *testing.T) {
	code, _, out, errOut, restored := runGuardedChild(t, "kill")
	require.Equal(t, 128+int(syscall.SIGKILL), code)
	require.True(t, restored)
	require.Contains(t, out.String(), terminalReset)
	require.Equal(t, "Crush was stopped (killed).\n", errOut.String())
}

func TestCrashGuardLeavesNormalExitAlone(t *testing.T) {
	code, report, out, errOut, restored := runGuardedChild(t, "exit")
	require.Equal(t, 3, code)
	require.False(t, restored)
	require.Empty(t, out.String())
	require.Empty(t, errOut.String())
	require.NoFileExists(t, report)
}
