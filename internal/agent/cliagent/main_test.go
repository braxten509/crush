package cliagent

import (
	"os"
	"testing"
)

// TestMain keeps saved image copies out of the real cache folder.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "cliagent-cache-")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_CACHE_HOME", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
