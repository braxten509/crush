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
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/charmbracelet/crush/internal/lock"
)

// lockWait bounds how long a change waits for another process's.
const lockWait = 5 * time.Second

// pathLocks queues changes within this process, so they wait their turn
// instead of polling the file lock; the file lock orders processes.
var pathLocks sync.Map // path -> *sync.Mutex

// Read returns the value saved at path, or the zero value when the file is
// missing or unreadable.
func Read[T any](path string) T {
	var value T
	data, err := os.ReadFile(path)
	if err != nil {
		return value
	}
	if err := json.Unmarshal(data, &value); err != nil {
		slog.Warn("Discarding an unreadable saved file", "path", path, "error", err)
		var zero T
		return zero
	}
	return value
}

// Update changes the value saved at path under its lock. change gets the
// saved value (the zero value when there is none) and reports whether to
// write it back. An empty result (null, [] or {}) removes the file.
func Update[T any](path string, change func(*T) bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	mu, _ := pathLocks.LoadOrStore(path, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), lockWait)
	defer cancel()
	release, err := lock.File(ctx, path+".lock")
	if err != nil {
		return err
	}
	defer release()

	value := Read[T](path)
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
