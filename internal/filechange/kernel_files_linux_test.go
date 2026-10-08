//go:build linux

package filechange

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTrackSkipsDevicesAndKernelFiles(t *testing.T) {
	root := visibleRestoreRoot(t)
	tracker, err := New(t.Context(), root)
	require.NoError(t, err)
	skipped := []string{"/dev/null", "/dev/tty", "/proc/self/oom_score_adj", "/sys/kernel/mm/transparent_hugepage/enabled"}
	if self, err := os.ReadFile("/proc/self/cgroup"); err == nil && len(self) > 0 {
		skipped = append(skipped, "/sys/fs/cgroup/cgroup.procs")
	}
	for _, path := range skipped {
		tracker.Track(path)
		require.False(t, tracker.Contains(path), path)
	}
	file := filepath.Join(root, "real.txt")
	tracker.Track(file)
	require.True(t, tracker.Contains(file), "a new regular file is still tracked")
}
