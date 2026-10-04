//go:build !windows

package filehistory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPinnedParentSwapCannotRedirectRestore(t *testing.T) {
	for _, action := range []string{"read", "replace", "delete", "recreate"} {
		t.Run(action, func(t *testing.T) {
			root := restoreRaceRoot(t)
			parent, held, outside := filepath.Join(root, "selected"), filepath.Join(root, "held"), filepath.Join(root, "outside")
			require.NoError(t, os.Mkdir(parent, 0700))
			require.NoError(t, os.Mkdir(outside, 0700))
			path := filepath.Join(parent, "file")
			if action != "recreate" {
				require.NoError(t, os.WriteFile(path, []byte("selected bytes"), 0600))
			}
			require.NoError(t, os.WriteFile(filepath.Join(outside, "file"), []byte("outside bytes"), 0600))
			swapped := false
			target, err := walkRestoreParents(path, true, func(opened string) {
				if opened == parent {
					require.NoError(t, os.Rename(parent, held))
					require.NoError(t, os.Symlink(outside, parent))
					swapped = true
				}
			})
			require.NoError(t, err)
			defer target.close()
			require.True(t, swapped)
			before, data, err := target.current()
			require.NoError(t, err)
			if action == "recreate" {
				require.False(t, before.Exists)
			} else {
				require.Equal(t, "selected bytes", string(data))
			}
			switch action {
			case "replace", "recreate":
				require.NoError(t, target.replace([]byte("restored"), 0600, before))
				require.Equal(t, "restored", contents(t, filepath.Join(held, "file")))
			case "delete":
				require.NoError(t, target.delete(before))
				require.NoFileExists(t, filepath.Join(held, "file"))
			}
			require.Equal(t, "outside bytes", contents(t, filepath.Join(outside, "file")))
			entries, err := os.ReadDir(outside)
			require.NoError(t, err)
			require.Len(t, entries, 1, "no restore temporary file outside the selected directory")
			_, _, err = currentState(path)
			require.Error(t, err, "a fresh traversal must reject the new symlink")
		})
	}
}

func TestMissingParentsStayUnderPinnedAncestor(t *testing.T) {
	root := restoreRaceRoot(t)
	parent, held, outside := filepath.Join(root, "selected"), filepath.Join(root, "held"), filepath.Join(root, "outside")
	require.NoError(t, os.Mkdir(parent, 0700))
	require.NoError(t, os.Mkdir(outside, 0700))
	path := filepath.Join(parent, "new", "nested", "file")
	target, err := walkRestoreParents(path, true, func(opened string) {
		if opened == parent {
			require.NoError(t, os.Rename(parent, held))
			require.NoError(t, os.Symlink(outside, parent))
		}
	})
	require.NoError(t, err)
	defer target.close()
	require.NoError(t, target.replace([]byte("restored"), 0600, State{}))
	require.Equal(t, "restored", contents(t, filepath.Join(held, "new", "nested", "file")))
	require.NoDirExists(t, filepath.Join(outside, "new"))
}

func TestPinnedTargetRejectsLeafLinkAndChangedFile(t *testing.T) {
	root := restoreRaceRoot(t)
	path, outside := filepath.Join(root, "file"), filepath.Join(root, "outside")
	require.NoError(t, os.WriteFile(path, []byte("original"), 0600))
	require.NoError(t, os.WriteFile(outside, []byte("outside"), 0600))
	target, err := openRestoreTarget(path, false)
	require.NoError(t, err)
	defer target.close()
	before, _, err := target.current()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte("changed"), 0600))
	require.ErrorIs(t, target.replace([]byte("restore"), 0600, before), errRestoreChanged)
	require.ErrorIs(t, target.delete(before), errRestoreChanged)
	require.Equal(t, "changed", contents(t, path))
	require.NoError(t, os.Remove(path))
	require.NoError(t, os.Symlink(outside, path))
	_, _, err = target.current()
	require.Error(t, err)
	require.ErrorIs(t, target.replace([]byte("restore"), 0600, before), errRestoreChanged)
	require.ErrorIs(t, target.delete(before), errRestoreChanged)
	require.Equal(t, "outside", contents(t, outside))
}

// Restore intentionally excludes the operating system temp directory.
func restoreRaceRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp(".", "restore-race-")
	require.NoError(t, err)
	root, err = filepath.Abs(root)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(root)) })
	return root
}
