package filechange

import (
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Pending baselines may outlive many CLI turns without ever changing. Keep
// their bytes in private scratch storage, not in the long-lived tracker map.
// Once a review owns the payload it is handed to durable history; the scratch
// file is removed when its last baseline handle is collected.
type restorePayload struct{ path string }

func spillRestore(value entry) entry {
	if value.state.RestoreData == "" {
		return value
	}
	data, err := base64.StdEncoding.DecodeString(value.state.RestoreData)
	value.state.RestoreData = ""
	if err == nil {
		var directory string
		directory, err = stagingDirectory()
		if err == nil {
			var file *os.File
			file, err = os.CreateTemp(directory, "snapshot-*")
			if err == nil {
				_, err = file.Write(data)
				closeErr := file.Close()
				if err == nil {
					err = closeErr
				}
				if err == nil {
					payload := &restorePayload{path: file.Name()}
					runtime.AddCleanup(payload, func(path string) { _ = os.Remove(path) }, payload.path)
					value.restore = payload
				} else {
					_ = os.Remove(file.Name())
				}
			}
		}
	}
	if err != nil {
		value.state.RestoreOmitted = "Full file contents could not be saved: " + err.Error()
	}
	return value
}

// Scratch files are removed when Go collects their handles, which never
// happens if Crush exits or crashes first. Each Crush therefore keeps them
// in a folder named after its process ID, and the first spill of a Crush
// removes the folders of Crushes that are gone.
var (
	stagingOnce sync.Once
	stagingDir  string
	stagingErr  error
	// stagingBase is where the folders live; tests point it elsewhere.
	stagingBase = os.UserCacheDir
)

// looseStagingAge spares loose files of Crush builds from before the
// per-process folders while they may still be running.
const looseStagingAge = 24 * time.Hour

func stagingDirectory() (string, error) {
	stagingOnce.Do(func() {
		var root string
		root, stagingErr = stagingBase()
		if stagingErr != nil {
			return
		}
		root = filepath.Join(root, "crush", "restore-staging")
		sweepStaging(root, time.Now())
		stagingDir = filepath.Join(root, strconv.Itoa(os.Getpid()))
		stagingErr = os.MkdirAll(stagingDir, 0o700)
	})
	return stagingDir, stagingErr
}

// sweepStaging removes the folders of processes that are gone and loose
// files older than looseStagingAge.
func sweepStaging(root string, now time.Time) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		if entry.IsDir() {
			if pid, err := strconv.Atoi(entry.Name()); err == nil && pid > 0 && pid != os.Getpid() && !processAlive(pid) {
				_ = os.RemoveAll(path)
			}
			continue
		}
		if info, err := entry.Info(); err == nil && strings.HasPrefix(entry.Name(), "snapshot-") && now.Sub(info.ModTime()) > looseStagingAge {
			_ = os.Remove(path)
		}
	}
}

// processAlive reports whether pid runs. Errors other than "no such
// process" count as running, so a folder is never removed by mistake.
func processAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, syscall.ESRCH)
}

func (value entry) reviewState(budget *int) State {
	state := value.state
	if value.restore == nil {
		return state
	}
	if state.Size > int64(*budget) {
		state.RestoreOmitted = RestoreBudgetReason
		return state
	}
	file, err := os.Open(value.restore.path)
	var data []byte
	if err == nil {
		data, err = io.ReadAll(io.LimitReader(file, MaxRestoreSize+1))
		file.Close()
	}
	runtime.KeepAlive(value.restore)
	if err != nil || len(data) > MaxRestoreSize || digest(data) != state.Digest {
		state.RestoreOmitted = "Full file contents could not be read from scratch storage"
		return state
	}
	if len(data) > *budget {
		state.RestoreOmitted = RestoreBudgetReason
		return state
	}
	*budget -= len(data)
	state.RestoreData = base64.StdEncoding.EncodeToString(data)
	return state
}
