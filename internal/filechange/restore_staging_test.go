package filechange

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A process that has already exited, for a PID nothing runs under.
func exitedPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	require.NoError(t, cmd.Run())
	return cmd.Process.Pid
}

// Folders of gone Crushes and old loose files go; live Crushes' folders,
// recent loose files and anything else stay.
func TestSweepStagingRemovesWhatGoneCrushesLeft(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	write := func(path string, age time.Duration) {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))
		require.NoError(t, os.Chtimes(path, now.Add(-age), now.Add(-age)))
	}
	gone := filepath.Join(root, strconv.Itoa(exitedPID(t)))
	live := filepath.Join(root, strconv.Itoa(os.Getppid()))
	write(filepath.Join(gone, "snapshot-1"), time.Minute)
	write(filepath.Join(live, "snapshot-2"), 72*time.Hour)
	write(filepath.Join(root, "snapshot-old"), 48*time.Hour)
	write(filepath.Join(root, "snapshot-new"), time.Hour)
	write(filepath.Join(root, "other", "keep"), 72*time.Hour)
	write(filepath.Join(root, "unrelated-old"), 72*time.Hour)

	sweepStaging(root, now)

	for path, kept := range map[string]bool{
		gone:                                 false,
		live:                                 true,
		filepath.Join(root, "snapshot-old"):  false,
		filepath.Join(root, "snapshot-new"):  true,
		filepath.Join(root, "other"):         true,
		filepath.Join(root, "unrelated-old"): true,
	} {
		_, err := os.Stat(path)
		if kept {
			require.NoError(t, err, path)
		} else {
			require.ErrorIs(t, err, os.ErrNotExist, path)
		}
	}
}

func TestProcessAlive(t *testing.T) {
	require.True(t, processAlive(os.Getpid()))
	require.False(t, processAlive(exitedPID(t)))
}

// Spilled copies land in this process's own folder and read back.
func TestSpillRestoreUsesOwnFolder(t *testing.T) {
	directory, err := stagingDirectory()
	require.NoError(t, err)
	require.Equal(t, strconv.Itoa(os.Getpid()), filepath.Base(directory))
	data := []byte("before contents")
	value := spillRestore(entry{state: State{RestoreData: base64.StdEncoding.EncodeToString(data), Size: int64(len(data)), Digest: digest(data)}})
	require.NotNil(t, value.restore)
	require.Equal(t, directory, filepath.Dir(value.restore.path))
	budget := 1 << 20
	require.Equal(t, base64.StdEncoding.EncodeToString(data), value.reviewState(&budget).RestoreData)
}
