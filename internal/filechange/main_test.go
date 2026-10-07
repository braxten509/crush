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
	os.Exit(m.Run())
}
