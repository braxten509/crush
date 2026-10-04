package filechange

import (
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"runtime"
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
		directory, err = os.UserCacheDir()
		if err == nil {
			directory = filepath.Join(directory, "crush", "restore-staging")
			err = os.MkdirAll(directory, 0700)
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
	}
	if err != nil {
		value.state.RestoreOmitted = "Full file contents could not be saved: " + err.Error()
	}
	return value
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
