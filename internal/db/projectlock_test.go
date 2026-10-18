package db

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/lock"
	"github.com/stretchr/testify/require"
)

// TestLoneLockRefusedWhileEngineRuns proves a lone window is refused while
// an engine holds the project, with the engine's PID read from the owner
// info the engine wrote.
func TestLoneLockRefusedWhileEngineRuns(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()

	release, err := LockProject(t.Context(), dataDir, ProjectLockEngine, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = release() })

	_, err = LockProject(t.Context(), dataDir, ProjectLockAlone, 0)
	require.ErrorIs(t, err, ErrDataDirLocked)
	require.Equal(t, fmt.Sprintf(MsgEngineHoldsProject, os.Getpid()), err.Error())
}

// TestEngineLockRefusedByLoneWindow proves the engine tells a lone window's
// shared lock apart from another engine: it reports the lone-window message.
func TestEngineLockRefusedByLoneWindow(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()

	release, err := LockProject(t.Context(), dataDir, ProjectLockAlone, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = release() })

	start := time.Now()
	_, err = LockProject(t.Context(), dataDir, ProjectLockEngine, 300*time.Millisecond)
	require.ErrorIs(t, err, ErrDataDirLocked)
	require.Equal(t, MsgLoneWindowHoldsProject, err.Error())
	require.GreaterOrEqual(t, time.Since(start), 300*time.Millisecond, "the engine waits out its wait first")
}

// TestEngineLockRefusedByOtherEngine proves a second engine is told another
// engine holds the project, with that engine's PID.
func TestEngineLockRefusedByOtherEngine(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()

	release, err := LockProject(t.Context(), dataDir, ProjectLockEngine, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = release() })

	_, err = LockProject(t.Context(), dataDir, ProjectLockEngine, 0)
	require.ErrorIs(t, err, ErrDataDirLocked)
	require.Equal(t, fmt.Sprintf(MsgOtherEngineHoldsProject, os.Getpid()), err.Error())
}

// TestLoneWindowsCoexist proves two lone windows share the project.
func TestLoneWindowsCoexist(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()

	first, err := LockProject(t.Context(), dataDir, ProjectLockAlone, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = first() })
	second, err := LockProject(t.Context(), dataDir, ProjectLockAlone, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = second() })
}

// TestEngineWaitsForPreviousEngine proves a new engine waits for an exiting
// one: a holder that lets go after 1 s hands the lock over within the 5 s
// wait.
func TestEngineWaitsForPreviousEngine(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()

	previous, err := LockProject(t.Context(), dataDir, ProjectLockEngine, 0)
	require.NoError(t, err)
	time.AfterFunc(time.Second, func() { _ = previous() })

	start := time.Now()
	release, err := LockProject(t.Context(), dataDir, ProjectLockEngine, 5*time.Second)
	require.NoError(t, err, "the second engine must get the lock once the first lets go")
	t.Cleanup(func() { _ = release() })
	elapsed := time.Since(start)
	require.GreaterOrEqual(t, elapsed, 900*time.Millisecond, "it got the lock only after the holder let go")
	require.Less(t, elapsed, 5*time.Second)
}

// TestEngineOwnerInfoHasKind proves the engine records itself with
// "kind":"engine", and a lone window writes nothing.
func TestEngineOwnerInfoHasKind(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	path := filepath.Join(dataDir, dataDirLockFile)

	alone, err := LockProject(t.Context(), dataDir, ProjectLockAlone, 0)
	require.NoError(t, err)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Empty(t, raw, "a lone window never writes the owner info")
	require.NoError(t, alone())

	release, err := LockProject(t.Context(), dataDir, ProjectLockEngine, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = release() })
	raw, err = os.ReadFile(path)
	require.NoError(t, err)
	var info dataDirOwnerInfo
	require.NoError(t, json.Unmarshal(raw, &info))
	require.Equal(t, os.Getpid(), info.PID)
	require.Equal(t, "engine", info.Kind)
}

// TestProbeProjectLock proves ensure's probe sees an engine's exclusive lock,
// ignores a lone window's shared one, and treats a missing folder as free.
func TestProbeProjectLock(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()

	require.NoError(t, ProbeProjectLock(filepath.Join(dataDir, "not-created-yet")))
	require.NoError(t, ProbeProjectLock(dataDir))

	alone, err := LockProject(t.Context(), dataDir, ProjectLockAlone, 0)
	require.NoError(t, err)
	require.NoError(t, ProbeProjectLock(dataDir), "a lone window's shared lock does not block the probe")
	require.NoError(t, alone())

	release, err := LockProject(t.Context(), dataDir, ProjectLockEngine, 0)
	require.NoError(t, err)
	err = ProbeProjectLock(dataDir)
	require.ErrorIs(t, err, ErrDataDirLocked)
	require.Equal(t, fmt.Sprintf(MsgOtherEngineHoldsProject, os.Getpid()), err.Error())
	require.NoError(t, release())
	require.NoError(t, ProbeProjectLock(dataDir), "the probe released its own lock at once")
}

// TestSkipLockEnvSkipsBoth proves CRUSH_SKIP_DATADIR_LOCK skips the engine's
// and the lone window's lock alike, even while another holder has the file.
func TestSkipLockEnvSkipsBoth(t *testing.T) {
	dataDir := t.TempDir()
	holder, err := lock.TryFile(filepath.Join(dataDir, dataDirLockFile))
	require.NoError(t, err)
	t.Cleanup(holder)

	t.Setenv("CRUSH_SKIP_DATADIR_LOCK", "1")
	for _, mode := range []ProjectLockMode{ProjectLockEngine, ProjectLockAlone} {
		release, err := LockProject(context.Background(), dataDir, mode, 0)
		require.NoError(t, err, "mode %d", mode)
		require.NoError(t, release())
	}
	require.NoError(t, ProbeProjectLock(dataDir))
}
