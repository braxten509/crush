package cliagent

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain keeps saved image copies out of the real cache folder.
func TestMain(m *testing.M) {
	home, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}
	// The read-only sandbox test requires a project outside the OS temp dir.
	dir, err := os.MkdirTemp(home, "crush-cliagent-tests-")
	if err != nil {
		panic(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".cache"), 0o700); err != nil {
		os.RemoveAll(dir)
		panic(err)
	}
	os.Setenv("XDG_CACHE_HOME", filepath.Join(dir, ".cache"))
	os.Setenv("HOME", dir)
	os.Setenv("LocalAppData", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
