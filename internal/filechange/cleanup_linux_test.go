//go:build linux && amd64

package filechange

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestHiddenBaselineDoesNotOpenFile(t *testing.T) {
	root := visibleRestoreRoot(t)
	put(t, root, ".cache/library.js", "cached bytes")
	path := filepath.Join(root, ".cache", "library.js")
	watch, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	require.NoError(t, err)
	defer unix.Close(watch)
	_, err = unix.InotifyAddWatch(watch, path, unix.IN_OPEN)
	require.NoError(t, err)
	tracker, err := New(t.Context(), root)
	require.NoError(t, err)
	tracker.Track(path)
	_, err = tracker.Checkpoint(t.Context())
	require.NoError(t, err)
	buffer := make([]byte, 4096)
	_, err = unix.Read(watch, buffer)
	require.ErrorIs(t, err, unix.EAGAIN, "hidden files must not be opened to build a preview")
	// Verify the watch can see a real content read.
	_, err = os.ReadFile(path)
	require.NoError(t, err)
	count, err := unix.Read(watch, buffer)
	require.NoError(t, err)
	require.Positive(t, count)
}

func TestObservedBulkCacheCleanupDoesNotRecordIndividualDeletions(t *testing.T) {
	root := visibleRestoreRoot(t)
	for i := range 2000 {
		put(t, root, fmt.Sprintf(".cache/%d", i), "downloaded bytes")
	}
	review, err := runObserved(t, root, "python3 -c 'import shutil; shutil.rmtree(\".cache\")'")
	require.NoError(t, err)
	require.Nil(t, review, "silent script deletions do not enter the blocking review recorder")
	require.NoDirExists(t, filepath.Join(root, ".cache"))
}
