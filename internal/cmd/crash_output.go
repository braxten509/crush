package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"
)

// recordCrashes saves the runtime's report of a fatal crash, which otherwise
// only reaches the terminal the TUI was drawing over. It returns a function
// that removes the file again when the run ends without one.
func recordCrashes() func() {
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return func() {}
		}
		state = filepath.Join(home, ".local", "state")
	}
	dir := filepath.Join(state, "crush", "crashes")
	if os.MkdirAll(dir, 0o700) != nil {
		return func() {}
	}
	// Runs that were killed leave an empty file; drop those after a day.
	if entries, err := os.ReadDir(dir); err == nil {
		for _, entry := range entries {
			info, err := entry.Info()
			if err == nil && info.Size() == 0 && time.Since(info.ModTime()) > 24*time.Hour {
				_ = os.Remove(filepath.Join(dir, entry.Name()))
			}
		}
	}
	path := filepath.Join(dir, fmt.Sprintf("crash-%s-%d.log", time.Now().Format("20060102-150405"), os.Getpid()))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return func() {}
	}
	if debug.SetCrashOutput(file, debug.CrashOptions{}) != nil {
		file.Close()
		_ = os.Remove(path)
		return func() {}
	}
	return func() {
		if info, err := file.Stat(); err == nil && info.Size() == 0 {
			_ = os.Remove(path)
		}
	}
}
