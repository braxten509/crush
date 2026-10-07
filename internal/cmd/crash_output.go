package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	crushlog "github.com/charmbracelet/crush/internal/log"
)

// crashReportEnv carries the crash report path from the crash guard to the
// Crush it runs. Its presence also marks that Crush as already guarded.
const crashReportEnv = "CRUSH_CRASH_REPORT"

// recordCrashes saves the runtime's report of a fatal crash, which otherwise
// only reaches the terminal the TUI was drawing over. It returns the report
// file (nil when it couldn't be set up) and a function that removes the
// file again when the run ends without one.
func recordCrashes() (*os.File, func()) {
	dir, err := crushlog.CrashDir()
	if err != nil {
		return nil, func() {}
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
	path := os.Getenv(crashReportEnv)
	if path == "" {
		path = filepath.Join(dir, fmt.Sprintf("crash-%s-%d.log", time.Now().Format("20060102-150405"), os.Getpid()))
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, func() {}
	}
	if debug.SetCrashOutput(file, debug.CrashOptions{}) != nil {
		file.Close()
		_ = os.Remove(path)
		return nil, func() {}
	}
	return file, func() {
		if info, err := file.Stat(); err == nil && info.Size() == 0 {
			_ = os.Remove(path)
		}
	}
}
