package log

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// CrashDir returns the folder crash reports are saved in, creating it if
// needed: $XDG_STATE_HOME/crush/crashes, or ~/.local/state/crush/crashes.
func CrashDir() (string, error) {
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		state = filepath.Join(home, ".local", "state")
	}
	dir := filepath.Join(state, "crush", "crashes")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// SavePanicReport writes a recovered panic and its stack to a new file in
// CrashDir and returns the file's path, or "" when it could not be saved.
func SavePanicReport(name string, r any, stack []byte) string {
	dir, err := CrashDir()
	if err != nil {
		return ""
	}
	now := time.Now()
	path := filepath.Join(dir, fmt.Sprintf("panic-%s-%s-%d.log", name, now.Format("20060102-150405"), os.Getpid()))
	report := fmt.Sprintf("Panic in %s: %v\n\nTime: %s\n\nStack Trace:\n%s\n", name, r, now.Format(time.RFC3339), stack)
	if os.WriteFile(path, []byte(report), 0o600) != nil {
		return ""
	}
	return path
}
