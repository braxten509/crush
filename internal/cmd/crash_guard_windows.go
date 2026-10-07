//go:build windows

package cmd

import "os"

// runCrashGuarded is a no-op on Windows, whose console has no TUI modes
// left behind by a crash.
func runCrashGuarded() (exitCode int, guarded bool) { return 0, false }

func quietStderr(*os.File) (restore func()) { return func() {} }
