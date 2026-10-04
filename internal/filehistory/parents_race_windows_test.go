package filehistory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWindowsParentsStayLockedThroughRestore(t *testing.T) {
	root, err := os.MkdirTemp(".", "restore-race-")
	require.NoError(t, err)
	root, err = filepath.Abs(root)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(root)) })
	parent := filepath.Join(root, "selected")
	require.NoError(t, os.Mkdir(parent, 0700))
	target, err := walkRestoreParents(filepath.Join(parent, "new", "nested", "file"), true, func(opened string) {
		if opened == parent {
			require.Error(t, os.Rename(parent, filepath.Join(root, "moved")), "opened ancestor cannot be replaced while missing children are created")
		}
	})
	require.NoError(t, err)
	defer target.close()
	require.Error(t, os.Rename(parent, filepath.Join(root, "moved")))
	require.NoError(t, target.replace([]byte("restored"), 0600, State{}))
	before, data, err := target.current()
	require.NoError(t, err)
	require.Equal(t, "restored", string(data))
	require.NoError(t, target.replace([]byte("updated"), 0600, before))
	before, _, err = target.current()
	require.NoError(t, err)
	require.NoError(t, target.delete(before))
	target.close()
	target.parents = nil
	require.NoError(t, os.Rename(parent, filepath.Join(root, "moved")), "closing the target releases all ancestor locks")
}
