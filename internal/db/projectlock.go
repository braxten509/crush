package db

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/charmbracelet/crush/internal/lock"
)

// ProjectLockMode says how [LockProject] holds a project's lock
// (engine-design.md 5.10).
type ProjectLockMode int

const (
	// ProjectLockEngine is exclusive, held for the engine's whole life.
	ProjectLockEngine ProjectLockMode = iota
	// ProjectLockAlone is shared, held by a terminal window that works
	// alone. Lone windows coexist; they and the engine exclude each other.
	ProjectLockAlone
)

// The messages a refused [LockProject] reports, exactly as each side shows
// them. The %d is the PID from the lock file's owner info.
const (
	MsgEngineHoldsProject      = "This project is open in the Crush engine (PID %d) that the desktop app and shared terminal windows use, so Crush can't also work alone here. Close those windows, then try again."
	MsgLoneWindowHoldsProject  = "This project is open in a Crush terminal window that works alone, so the shared engine can't start. Close that window, then try again."
	MsgOtherEngineHoldsProject = "This project is already open in another Crush engine (PID %d). Close its windows, or wait a minute, then try again."
)

// ownerKindEngine marks owner info written by an engine.
const ownerKindEngine = "engine"

// projectLockRetry is how often a waiting LockProject tries again.
const projectLockRetry = 100 * time.Millisecond

// projectLockedError is a refused LockProject. Its text is exactly one of
// the Msg* messages, and it wraps [ErrDataDirLocked].
type projectLockedError struct{ msg string }

func (e *projectLockedError) Error() string { return e.msg }
func (e *projectLockedError) Unwrap() error { return ErrDataDirLocked }

// LockProject takes the lock on <dataDir>/crush.lock. The engine waits up to
// wait for it; a lone window passes 0. On conflict it returns an error
// wrapping ErrDataDirLocked whose text is one of the messages below.
//
// The engine records its owner info (with "kind":"engine") once it holds
// the lock; lone windows never write the file, because several shared
// holders would overwrite each other. CRUSH_SKIP_DATADIR_LOCK skips both
// modes. The lock is a flock, so it dies with the process.
func LockProject(ctx context.Context, dataDir string, mode ProjectLockMode, wait time.Duration) (release func() error, err error) {
	if skipDataDirLock() {
		return func() error { return nil }, nil
	}
	path := filepath.Join(dataDir, dataDirLockFile)
	try := lock.TryFile
	if mode == ProjectLockAlone {
		try = lock.TryFileShared
	}
	deadline := time.Now().Add(wait)
	for {
		rel, err := try(path)
		if err == nil {
			if mode == ProjectLockEngine {
				if err := writeEngineOwnerInfo(path); err != nil {
					slog.Debug("Failed to write the project lock's owner info", "path", path, "error", err)
				}
			}
			return func() error { rel(); return nil }, nil
		}
		if !errors.Is(err, lock.ErrContended) {
			return nil, fmt.Errorf("failed to lock project %q: %w", dataDir, err)
		}
		if !time.Now().Before(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("failed to lock project %q: %w", dataDir, ctx.Err())
		case <-time.After(min(projectLockRetry, time.Until(deadline))):
		}
	}
	if mode == ProjectLockAlone {
		// A shared lock is only refused by an exclusive holder: an engine,
		// or the legacy server, which counts as one.
		return nil, &projectLockedError{msg: fmt.Sprintf(MsgEngineHoldsProject, readOwnerInfo(path).PID)}
	}
	return lockEngineContended(path)
}

// lockEngineContended tells who refused the engine's exclusive lock. A
// shared lock on a new descriptor succeeds only when the holders are lone
// windows; when it fails, another engine holds the project. A holder that
// let go in between is caught by one last exclusive try, which then wins.
func lockEngineContended(path string) (func() error, error) {
	shared, err := lock.TryFileShared(path)
	if err != nil {
		if errors.Is(err, lock.ErrContended) {
			return nil, &projectLockedError{msg: fmt.Sprintf(MsgOtherEngineHoldsProject, readOwnerInfo(path).PID)}
		}
		return nil, fmt.Errorf("failed to lock project %q: %w", filepath.Dir(path), err)
	}
	shared()
	if rel, err := lock.TryFile(path); err == nil {
		if err := writeEngineOwnerInfo(path); err != nil {
			slog.Debug("Failed to write the project lock's owner info", "path", path, "error", err)
		}
		return func() error { rel(); return nil }, nil
	}
	return nil, &projectLockedError{msg: MsgLoneWindowHoldsProject}
}

// ProbeProjectLock reports whether another process holds dataDir's project
// lock exclusively: an engine, or the legacy server, which counts as one.
// It probes with a shared lock on a new descriptor and releases it at once,
// so a lone window's shared lock does not count. When the lock is held it
// returns an error wrapping ErrDataDirLocked whose text is
// MsgOtherEngineHoldsProject for the PID in the owner info. A data folder
// that does not exist yet, and CRUSH_SKIP_DATADIR_LOCK, mean it is free.
func ProbeProjectLock(dataDir string) error {
	if skipDataDirLock() {
		return nil
	}
	path := filepath.Join(dataDir, dataDirLockFile)
	release, err := lock.TryFileShared(path)
	switch {
	case err == nil:
		release()
		return nil
	case errors.Is(err, lock.ErrContended):
		return &projectLockedError{msg: fmt.Sprintf(MsgOtherEngineHoldsProject, readOwnerInfo(path).PID)}
	case errors.Is(err, fs.ErrNotExist):
		return nil
	default:
		return fmt.Errorf("failed to check the lock of project %q: %w", dataDir, err)
	}
}

// writeEngineOwnerInfo records this process as the engine holding path.
func writeEngineOwnerInfo(path string) error {
	return writeOwnerInfoKind(path, ownerKindEngine)
}
