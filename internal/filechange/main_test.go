package filechange

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	// Keep ordinary fixtures on physical paths; explicit alias tests below
	// exercise directory symlinks separately on every supported platform.
	if dir, err := filepath.EvalSymlinks(os.TempDir()); err == nil {
		os.Setenv("TMPDIR", dir)
	}
	// Spilled restore copies stay out of the user's real cache folder.
	cache, err := os.MkdirTemp("", "filechange-cache-")
	if err != nil {
		panic(err)
	}
	stagingBase = func() (string, error) { return cache, nil }
	code := m.Run()
	os.RemoveAll(cache)
	os.Exit(code)
}
