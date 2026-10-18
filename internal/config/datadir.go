package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// ResolveDataDir applies the rules of defaults.go:71-80 without loading
// providers. It reads options.data_directory from the same files, with the
// same precedence, as Load: the same file lookup and merge, then the same
// setDefaults rule. An explicit dataDir wins, so then no file is read.
func ResolveDataDir(workingDir, dataDir string) (string, error) {
	cfg := &Config{}
	if dataDir == "" {
		loaded, _, err := loadFromConfigPaths(context.Background(), lookupConfigs(workingDir))
		if err != nil {
			return "", fmt.Errorf("failed to read the data folder from the config: %w", err)
		}
		cfg = loaded
	}
	cfg.setDefaults(workingDir, dataDir)
	return cfg.Options.DataDirectory, nil
}

// CanonicalDataDir makes d absolute and clean, then resolves symlinks in the
// longest part of it that exists: filepath.EvalSymlinks on that existing
// ancestor, with the rest of the path appended unchanged. A new project's
// data folder does not exist before its first engine starts, so this gives
// the same name before and after the folder is created, even under a
// symlinked parent (engine-design.md 5.2). server.CanonicalDataDir is this
// function; it lives here so the backend can use it too.
func CanonicalDataDir(d string) (string, error) {
	abs, err := filepath.Abs(d)
	if err != nil {
		return "", err
	}
	existing := abs
	var rest []string
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			break
		}
		rest = append(rest, filepath.Base(existing))
		existing = parent
	}
	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", err
	}
	slices.Reverse(rest)
	return filepath.Join(append([]string{resolved}, rest...)...), nil
}
