package filehistory

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func swapRestoreParent(t *testing.T, directory, moved, outside string) bool {
	t.Helper()
	err := os.Rename(directory, moved)
	if runtime.GOOS == "windows" {
		require.Error(t, err, "Windows must deny moving a pinned directory")
		return false
	}
	require.NoError(t, err)
	require.NoError(t, os.Symlink(outside, directory))
	return true
}

func TestPinnedRestoreParentDoesNotFollowReplacement(t *testing.T) {
	for _, action := range []string{"read", "replace", "delete", "recreate"} {
		t.Run(action, func(t *testing.T) {
			f := setup(t)
			directory, outside := filepath.Join(f.root, "selected"), filepath.Join(f.root, "outside")
			require.NoError(t, os.MkdirAll(directory, 0755))
			require.NoError(t, os.MkdirAll(outside, 0755))
			path := filepath.Join(directory, "file")
			outsidePath := filepath.Join(outside, "file")
			f.write(t, outsidePath, "outside bytes must stay untouched")
			if action != "recreate" {
				f.write(t, path, "selected bytes")
			}
			target, err := openRestoreTarget(path, false)
			require.NoError(t, err)
			defer target.close()
			before, _, err := target.current()
			require.NoError(t, err)
			moved := filepath.Join(f.root, "moved")
			swapped := swapRestoreParent(t, directory, moved, outside)
			actual := path
			if swapped {
				actual = filepath.Join(moved, "file")
			}
			switch action {
			case "read":
				now, data, err := target.current()
				require.NoError(t, err)
				require.Equal(t, before, now)
				require.Equal(t, "selected bytes", string(data))
			case "replace", "recreate":
				require.NoError(t, target.replace([]byte("restored bytes"), 0600, before))
				require.Equal(t, "restored bytes", contents(t, actual))
			case "delete":
				require.NoError(t, target.delete(before))
				require.NoFileExists(t, actual)
			}
			require.Equal(t, "outside bytes must stay untouched", contents(t, outsidePath))
			files, err := os.ReadDir(outside)
			require.NoError(t, err)
			require.Len(t, files, 1, "no temporary files in outside directory")
		})
	}
}

func TestMissingRestoreParentsUsePinnedAncestor(t *testing.T) {
	f := setup(t)
	directory, outside := filepath.Join(f.root, "selected"), filepath.Join(f.root, "outside")
	moved := filepath.Join(f.root, "moved")
	require.NoError(t, os.Mkdir(directory, 0755))
	require.NoError(t, os.Mkdir(outside, 0755))
	swapped := false
	target, err := walkRestoreParents(filepath.Join(directory, "missing", "nested", "file"), true, func(opened string) {
		if opened == directory {
			swapped = swapRestoreParent(t, directory, moved, outside)
		}
	})
	require.NoError(t, err)
	defer target.close()
	require.NoError(t, target.replace([]byte("recreated"), 0600, State{}))
	actual := directory
	if swapped {
		actual = moved
	}
	require.Equal(t, "recreated", contents(t, filepath.Join(actual, "missing", "nested", "file")))
	require.NoDirExists(t, filepath.Join(outside, "missing"))
}

func TestPinnedRestoreRejectsRacedLeafSymlink(t *testing.T) {
	f := setup(t)
	directory := filepath.Join(f.root, "selected")
	require.NoError(t, os.Mkdir(directory, 0755))
	path, outside := filepath.Join(directory, "file"), filepath.Join(f.root, "outside")
	f.write(t, path, "before")
	f.write(t, outside, "untouched")
	target, err := openRestoreTarget(path, false)
	require.NoError(t, err)
	defer target.close()
	before, _, err := target.current()
	require.NoError(t, err)
	require.NoError(t, os.Remove(path))
	require.NoError(t, os.Symlink(outside, path))
	_, data, err := target.current()
	require.Error(t, err)
	require.Empty(t, data)
	require.ErrorIs(t, target.replace([]byte("restore"), 0600, before), errRestoreChanged)
	require.ErrorIs(t, target.delete(before), errRestoreChanged)
	require.Equal(t, "untouched", contents(t, outside))
}

func TestPinnedRestoreRechecksOrdinaryConcurrentEdit(t *testing.T) {
	f := setup(t)
	path := filepath.Join(f.root, "file")
	f.write(t, path, "before")
	target, err := openRestoreTarget(path, false)
	require.NoError(t, err)
	defer target.close()
	before, _, err := target.current()
	require.NoError(t, err)
	f.write(t, path, "concurrent edit")
	require.ErrorIs(t, target.replace([]byte("restore"), 0600, before), errRestoreChanged)
	require.ErrorIs(t, target.delete(before), errRestoreChanged)
	require.Equal(t, "concurrent edit", contents(t, path))
}
