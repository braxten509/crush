//go:build linux

package filechange

import (
	"encoding/binary"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

// Observe actual filesystem access, not just the resulting diff: a discarded
// directory scan would still block chat even if it returned only named files.
//
// Inotify cannot tell which process opened a path, and the fixture lives in
// the checkout, where a concurrent `git status` (an editor, a shell prompt, an
// agent harness) lists untracked directories. The tracker is deterministic, so
// a real regression fails every attempt while outside noise does not.
func TestReviewNeverOpensUnreportedFilesOrDirectories(t *testing.T) {
	t.Parallel()
	root := visibleRestoreRoot(t)
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
	var newTurn, edit []string
	for range 5 {
		newTurn, edit = observeReviewOpens(t, root, fd)
		if len(newTurn) == 0 && !slices.ContainsFunc(edit, func(name string) bool { return name != "named.txt" }) {
			return
		}
	}
	require.Empty(t, newTurn, "a new turn must not open any file or directory")
	for _, name := range edit {
		require.Equal(t, "named.txt", name, "only the editing tool's file may be opened")
	}
}

// observeReviewOpens runs one fresh turn and one explicit edit, returning the
// names opened during each (an empty name is a watched directory itself).
func observeReviewOpens(t *testing.T, root string, fd int) (newTurn, edit []string) {
	t.Helper()
	readInotifyNames(t, fd)
	tracker, err := New(t.Context(), root)
	require.NoError(t, err)
	_, err = tracker.Checkpoint(t.Context())
	require.NoError(t, err)
	newTurn = readInotifyNames(t, fd)
	tracker.Track("named.txt")
	tracker.Track("dependencies")
	_, err = tracker.Checkpoint(t.Context())
	require.NoError(t, err)
	return newTurn, readInotifyNames(t, fd)
}

// readInotifyNames drains every queued event without blocking.
func readInotifyNames(t *testing.T, fd int) []string {
	t.Helper()
	var names []string
	buf := make([]byte, 4096)
	for {
		n, err := syscall.Read(fd, buf)
		if errors.Is(err, syscall.EAGAIN) {
			return names
		}
		require.NoError(t, err)
		for offset := 0; offset < n; {
			length := int(binary.NativeEndian.Uint32(buf[offset+12 : offset+16]))
			names = append(names, strings.TrimRight(string(buf[offset+16:offset+16+length]), "\x00"))
			offset += 16 + length
		}
	}
}
