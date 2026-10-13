// Package jsonfile keeps small JSON files that several Crush processes
// share. Every change holds a lock file for the whole read, change and
// atomic replace, so concurrent changes never lose each other's writes and
// readers never see a half-written file.
package jsonfile

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/charmbracelet/crush/internal/lock"
)

// lockWait bounds how long a change waits for another process's.
const lockWait = 5 * time.Second

// pathLocks queues reads and changes within this process, so changes wait
// their turn instead of polling the file lock, and a read never meets a
// replace (which fails on Windows); the file lock orders processes.
var pathLocks sync.Map // path -> *sync.Mutex

// pathLock holds the in-process lock of path, cleaned so two spellings of
// one file share it, and returns the cleaned path and its unlock.
func pathLock(path string) (string, func()) {
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	mu, _ := pathLocks.LoadOrStore(path, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	return path, mu.(*sync.Mutex).Unlock
}

// Read returns the value saved at path, or the zero value when the file is
// missing or unreadable.
func Read[T any](path string) T {
	path, unlock := pathLock(path)
	defer unlock()
	value, err := read[T](path)
	if err != nil && !errors.Is(err, errDamaged) {
		slog.Warn("Cannot read a saved file", "path", path, "error", err)
	}
	return value
}

// errDamaged marks a file that exists but doesn't hold valid JSON.
var errDamaged = errors.New("damaged saved file")

func read[T any](path string) (T, error) {
	var value T
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return value, nil
	}
	if err != nil {
		return value, err
	}
	if err := json.Unmarshal(data, &value); err != nil {
		var zero T
		return zero, fmt.Errorf("%w: %w", errDamaged, err)
	}
	return value, nil
}

// Update changes the value saved at path under its lock. change gets the
// saved value (the zero value when there is none) and reports whether to
// write it back. An empty result (null, [] or {}) removes the file. A file
// that can't be read stops the change, so it is never overwritten; a
// damaged one is kept beside it as path.corrupt and the change starts
// from the zero value.
func Update[T any](path string, change func(*T) bool) error {
	path, unlock := pathLock(path)
	defer unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), lockWait)
	defer cancel()
	release, err := lock.File(ctx, path+".lock")
	if err != nil {
		return err
	}
	defer release()

	value, err := read[T](path)
	if errors.Is(err, errDamaged) {
		slog.Warn("Keeping a damaged saved file aside and starting fresh", "path", path, "copy", path+".corrupt", "error", err)
		if err := os.Rename(path, path+".corrupt"); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if !change(&value) {
		return nil
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if empty(data) {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func empty(data []byte) bool {
	data = bytes.TrimSpace(data)
	return bytes.Equal(data, []byte("null")) || bytes.Equal(data, []byte("[]")) || bytes.Equal(data, []byte("{}"))
}
