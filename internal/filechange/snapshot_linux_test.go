//go:build linux

package filechange

import (
	"encoding/binary"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

// Observe actual filesystem access, not just the resulting diff: a discarded
// directory scan would still block chat even if it returned only named files.
func TestReviewNeverOpensUnreportedFilesOrDirectories(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	put(t, root, "named.txt", "before")
	put(t, root, "unrelated.txt", "private")
	put(t, root, "dependencies/unrelated.txt", "private")
	fd, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	require.NoError(t, err)
	t.Cleanup(func() { _ = syscall.Close(fd) })
	for _, path := range []string{root, filepath.Join(root, "dependencies")} {
		_, err = syscall.InotifyAddWatch(fd, path, syscall.IN_OPEN|syscall.IN_ACCESS)
		require.NoError(t, err)
	}
	tracker, err := New(t.Context(), root)
	require.NoError(t, err)
	_, err = tracker.Checkpoint(t.Context())
	require.NoError(t, err)
	buf := make([]byte, 4096)
	_, err = syscall.Read(fd, buf)
	require.ErrorIs(t, err, syscall.EAGAIN, "a new turn must not open any file or directory")
	tracker.Track("named.txt")
	tracker.Track("dependencies")
	_, err = tracker.Checkpoint(t.Context())
	require.NoError(t, err)
	n, err := syscall.Read(fd, buf)
	require.NoError(t, err)
	for offset := 0; offset < n; {
		length := int(binary.NativeEndian.Uint32(buf[offset+12 : offset+16]))
		name := strings.TrimRight(string(buf[offset+16:offset+16+length]), "\x00")
		require.Equal(t, "named.txt", name, "only the editing tool's file may be opened")
		offset += 16 + length
	}
}
