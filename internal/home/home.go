// Package home provides utilities for dealing with the user's home directory.
package home

import (
	"cmp"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var homedir, homedirErr = userHomeDir()

// IsolatedTestEnv names the variable a test sets, after pointing HOME, the
// XDG folders and CRUSH_GLOBAL_* at its own scratch folders, so the test
// binary it runs again as crush (an engine, `crush logout`) uses those
// folders instead of making a scratch home of its own.
const IsolatedTestEnv = "CRUSH_TEST_ISOLATED_ENV"

// userHomeDir returns the real home, or under go test a scratch one so no
// test can read or rewrite the developer's files.
func userHomeDir() (string, error) {
	if !testing.Testing() || os.Getenv(IsolatedTestEnv) == "1" {
		return os.UserHomeDir()
	}
	for _, key := range testIgnoredEnv {
		os.Unsetenv(key) //nolint:errcheck
	}
	return os.MkdirTemp("", "crush-test-home-*")
}

// testIgnoredEnv would point tests at real dirs; tests set them via t.Setenv.
var testIgnoredEnv = []string{
	"CRUSH_GLOBAL_CONFIG",
	"CRUSH_GLOBAL_DATA",
	"XDG_CONFIG_HOME",
	"XDG_DATA_HOME",
	"XDG_CACHE_HOME",
}

func init() {
	if homedirErr != nil {
		slog.Error("Failed to get user home directory", "error", homedirErr)
	}
}

// Dir returns the user home directory.
func Dir() string {
	return homedir
}

// Config returns the user config directory.
func Config() string {
	return cmp.Or(
		os.Getenv("XDG_CONFIG_HOME"),
		filepath.Join(Dir(), ".config"),
	)
}

// Short replaces the actual home path from [Dir] with `~`.
func Short(p string) string {
	if homedir == "" || !strings.HasPrefix(p, homedir) {
		return p
	}
	if len(p) == len(homedir) {
		return "~"
	}
	if !os.IsPathSeparator(p[len(homedir)]) {
		return p
	}
	return filepath.Join("~", strings.TrimPrefix(p, homedir))
}

// Long replaces the `~` with actual home path from [Dir].
func Long(p string) string {
	if homedir == "" || !strings.HasPrefix(p, "~") {
		return p
	}
	if len(p) == 1 {
		return homedir
	}
	if !os.IsPathSeparator(p[1]) {
		return p
	}
	return strings.Replace(p, "~", homedir, 1)
}
