package server

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/charmbracelet/crush/internal/config"
)

// longestSocketName is the longest socket file name Crush puts in the
// private folder, an engine socket's. A folder is only used when this name
// fits in it within maxUnixSocketPathLen.
const longestSocketName = "engine-0123456789abcdef.sock"

// ErrNoPrivateSocketDir is returned when no candidate folder is private
// enough for the Crush socket (engine-design.md 5.1 d).
var ErrNoPrivateSocketDir = errors.New("no private folder for the Crush socket")

// Seams for the folder checks, so tests can stand in for a folder another
// user owns, a file system that ignores chmod, and the system's /tmp.
var (
	socketDirOwner  = fileOwner
	chmodSocketDir  = os.Chmod
	shortSocketBase = "/tmp"
)

// PrivateSocketDir returns the folder that holds Crush's sockets. It tries,
// in order: $XDG_RUNTIME_DIR/crush (Linux only, when XDG_RUNTIME_DIR is
// set); <os.TempDir()>/crush-<uid>; and /tmp/crush-<uid>, the last only when
// a socket path in one of the first two would be over the 104-byte limit.
// Bare /tmp is never used.
//
// A folder is created with mode 0700 and accepted only if it is a
// directory, not a symlink, owned by this user, with mode exactly 0700. A
// folder of ours with another mode is set to 0700 and checked again. A
// rejected folder is skipped with a warning.
//
// The 104-byte limit is checked with the longest socket name Crush puts in
// the folder, an engine socket's, so every engine socket fits.
func PrivateSocketDir() (string, error) {
	return privateSocketDir(longestSocketName)
}

// privateSocketDir is [PrivateSocketDir] for one socket name: a candidate
// counts as too long only when that name does not fit in it. [DefaultHost]
// checks its own crush.sock this way.
func privateSocketDir(name string) (string, error) {
	uid := os.Getuid()
	var candidates []string
	if runtime.GOOS == "linux" {
		if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
			candidates = append(candidates, filepath.Join(dir, "crush"))
		}
	}
	candidates = append(candidates, filepath.Join(os.TempDir(), fmt.Sprintf("crush-%d", uid)))

	var reasons []string
	tooLong := false
	for _, dir := range candidates {
		if len(filepath.Join(dir, name)) > maxUnixSocketPathLen {
			tooLong = true
			slog.Debug("Socket folder skipped: the path is too long", "dir", dir)
			reasons = append(reasons, fmt.Sprintf("%s: a socket path in it would be over %d bytes", dir, maxUnixSocketPathLen))
			continue
		}
		if reason := checkPrivateDir(dir, uid); reason != "" {
			slog.Warn("Unsafe socket folder skipped", "dir", dir, "reason", reason)
			reasons = append(reasons, dir+": "+reason)
			continue
		}
		return dir, nil
	}
	if tooLong {
		dir := filepath.Join(shortSocketBase, fmt.Sprintf("crush-%d", uid))
		reason := checkPrivateDir(dir, uid)
		if reason == "" {
			return dir, nil
		}
		slog.Warn("Unsafe socket folder skipped", "dir", dir, "reason", reason)
		reasons = append(reasons, dir+": "+reason)
	}
	return "", fmt.Errorf("%w: %s", ErrNoPrivateSocketDir, strings.Join(reasons, "; "))
}

// checkPrivateDir creates dir if needed and reports why it is not a private
// folder of uid, or "" when it is one. A folder of uid's with another mode
// is set to 0700 and checked again.
func checkPrivateDir(dir string, uid int) string {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "it cannot be created: " + err.Error()
	}
	for fixed := false; ; fixed = true {
		info, err := os.Lstat(dir)
		if err != nil {
			return err.Error()
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "it is a symlink"
		}
		if !info.IsDir() {
			return "it is not a folder"
		}
		owner, ok := socketDirOwner(info)
		if !ok {
			return "its owner cannot be checked"
		}
		if owner != uid {
			return fmt.Sprintf("it belongs to user %d", owner)
		}
		mode := info.Mode() & (os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky)
		if mode == 0o700 {
			return ""
		}
		if fixed {
			return fmt.Sprintf("its mode is %#o, not 0700", uint32(mode.Perm()))
		}
		if err := chmodSocketDir(dir, 0o700); err != nil {
			return "its mode cannot be set to 0700: " + err.Error()
		}
	}
}

// CanonicalDataDir makes d absolute and clean, then resolves symlinks in the
// longest part of it that exists: filepath.EvalSymlinks on that existing
// ancestor, with the rest of the path appended unchanged.
func CanonicalDataDir(d string) (string, error) {
	return config.CanonicalDataDir(d)
}

// EngineSocketPath is the project's engine socket:
// <PrivateSocketDir>/engine-<first 16 hex of sha256(canonical data dir)>.sock
func EngineSocketPath(dataDir string) (string, error) {
	canonical, err := CanonicalDataDir(dataDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve the data folder %q: %w", dataDir, err)
	}
	dir, err := PrivateSocketDir()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(canonical))
	return filepath.Join(dir, "engine-"+hex.EncodeToString(sum[:])[:16]+".sock"), nil
}

// EngineAddress is "unix://" + EngineSocketPath.
func EngineAddress(dataDir string) (string, error) {
	path, err := EngineSocketPath(dataDir)
	if err != nil {
		return "", err
	}
	return "unix://" + path, nil
}
