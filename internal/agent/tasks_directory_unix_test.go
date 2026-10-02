//go:build !windows

package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTaskBaseRejectsUnsafeDirectories(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"symlink", "public", "file"} {
		t.Run(kind, func(t *testing.T) {
			base := filepath.Join(t.TempDir(), "base")
			switch kind {
			case "symlink":
				require.NoError(t, os.Symlink(t.TempDir(), base))
			case "public":
				require.NoError(t, os.Mkdir(base, 0o700))
				require.NoError(t, os.Chmod(base, 0o777))
			case "file":
				require.NoError(t, os.WriteFile(base, nil, 0o600))
			}
			require.Error(t, privateTaskBase(base))
		})
	}
	base := filepath.Join(t.TempDir(), "safe")
	require.NoError(t, privateTaskBase(base))
	require.NoError(t, privateTaskBase(base))
	info, err := os.Lstat(base)
	require.NoError(t, err)
	foreign := *info.Sys().(*syscall.Stat_t)
	foreign.Uid++
	require.False(t, privateTaskDirectory(taskForeignInfo{FileInfo: info, stat: &foreign}))
}

type taskForeignInfo struct {
	os.FileInfo
	stat *syscall.Stat_t
}

func (info taskForeignInfo) Sys() any { return info.stat }

func TestTaskCleanupPreservesLiveProcesses(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	child := exec.Command("sh", "-c", "exit 0")
	require.NoError(t, child.Start())
	deadPID := child.Process.Pid
	require.NoError(t, child.Wait())
	require.False(t, taskProcessAlive(deadPID))
	dead := fmt.Sprintf("tasks-%d-dead", deadPID)
	live := fmt.Sprintf("tasks-%d-live", os.Getpid())
	for _, name := range []string{dead, live, "tasks-legacy", "unrelated"} {
		require.NoError(t, os.Mkdir(filepath.Join(base, name), 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(base, name, "request.req"), nil, 0o600))
	}
	outside := t.TempDir()
	link := fmt.Sprintf("tasks-%d-link", deadPID)
	require.NoError(t, os.Symlink(outside, filepath.Join(base, link)))
	removeStaleTaskDirectories(base)
	_, err := os.Lstat(filepath.Join(base, dead))
	require.ErrorIs(t, err, os.ErrNotExist)
	for _, name := range []string{live, "tasks-legacy", "unrelated", link} {
		_, err := os.Lstat(filepath.Join(base, name))
		require.NoError(t, err, name)
	}
	_, err = os.Stat(outside)
	require.NoError(t, err)
}
