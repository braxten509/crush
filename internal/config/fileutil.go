package config

import (
	"os"

	"github.com/charmbracelet/crush/internal/config/internal/fileutil"
)

// readFile reads a config or cache file, retrying transient failures. See
// fileutil.ReadFile.
func readFile(path string) ([]byte, error) {
	return fileutil.ReadFile(path)
}

// atomicWriteFile writes a config or cache file atomically. See
// fileutil.AtomicWriteFile.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	return fileutil.AtomicWriteFile(path, data, perm)
}
