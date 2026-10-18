//go:build !windows

package server

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// shortTempDir makes a temp folder with a short name, so socket paths in it
// stay under the 104-byte limit even when TMPDIR is long (the test runner's
// is). Call it before a test points TMPDIR elsewhere.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "cs")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// privateRoots points XDG_RUNTIME_DIR and TMPDIR at fresh short folders and
// returns the socket folders PrivateSocketDir tries, in order:
// <XDG_RUNTIME_DIR>/crush (Linux only), then <TMPDIR>/crush-<uid>.
func privateRoots(t *testing.T) []string {
	t.Helper()
	runtimeDir := shortTempDir(t)
	tmpDir := shortTempDir(t)
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	t.Setenv("TMPDIR", tmpDir)
	var candidates []string
	if runtime.GOOS == "linux" {
		candidates = append(candidates, filepath.Join(runtimeDir, "crush"))
	}
	return append(candidates, filepath.Join(tmpDir, fmt.Sprintf("crush-%d", os.Getuid())))
}

func requirePerm(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	require.NoError(t, err)
	require.Equal(t, want, info.Mode().Perm(), "mode of %s", path)
}

func TestSocketIsOwnerOnly(t *testing.T) {
	candidates := privateRoots(t)
	// A folder of ours left with a looser mode is set back to 0700.
	require.NoError(t, os.Mkdir(candidates[0], 0o755))
	require.NoError(t, os.Chmod(candidates[0], 0o755))

	dir, err := PrivateSocketDir()
	require.NoError(t, err)
	require.Equal(t, candidates[0], dir)
	requirePerm(t, dir, 0o700)

	path := filepath.Join(dir, "crush.sock")
	ln, _, err := listen("unix", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	info, err := os.Lstat(path)
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&os.ModeSocket, "%s is not a socket", path)
	requirePerm(t, path, 0o600)
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		require.IsType(t, peerCheckListener{}, ln, "the same-user check wraps the listener")
	}
}

func TestPrivateSocketDirRejectsUnsafe(t *testing.T) {
	cases := []struct {
		name   string
		reason string
		// setup makes first, the first folder PrivateSocketDir tries, unsafe.
		setup func(t *testing.T, first, spare string)
	}{
		{
			name:   "symlink",
			reason: "it is a symlink",
			setup: func(t *testing.T, first, spare string) {
				require.NoError(t, os.Symlink(spare, first))
			},
		},
		{
			name:   "another user's folder",
			reason: fmt.Sprintf("it belongs to user %d", os.Getuid()+1),
			setup: func(t *testing.T, first, _ string) {
				require.NoError(t, os.Mkdir(first, 0o700))
				old := socketDirOwner
				socketDirOwner = func(info os.FileInfo) (int, bool) {
					if info.Name() == filepath.Base(first) {
						return os.Getuid() + 1, true
					}
					return fileOwner(info)
				}
				t.Cleanup(func() { socketDirOwner = old })
			},
		},
		{
			name:   "0755 folder whose mode does not change",
			reason: "its mode is 0755, not 0700",
			setup: func(t *testing.T, first, _ string) {
				require.NoError(t, os.Mkdir(first, 0o755))
				require.NoError(t, os.Chmod(first, 0o755))
				old := chmodSocketDir
				chmodSocketDir = func(dir string, mode os.FileMode) error {
					if dir == first {
						return nil
					}
					return old(dir, mode)
				}
				t.Cleanup(func() { chmodSocketDir = old })
			},
		},
		{
			name:   "0755 folder that cannot be changed",
			reason: "its mode cannot be set to 0700: read-only file system",
			setup: func(t *testing.T, first, _ string) {
				require.NoError(t, os.Mkdir(first, 0o755))
				require.NoError(t, os.Chmod(first, 0o755))
				old := chmodSocketDir
				chmodSocketDir = func(dir string, mode os.FileMode) error {
					if dir == first {
						return errors.New("read-only file system")
					}
					return old(dir, mode)
				}
				t.Cleanup(func() { chmodSocketDir = old })
			},
		},
		{
			name:   "a file",
			reason: "it cannot be created",
			setup: func(t *testing.T, first, _ string) {
				require.NoError(t, os.WriteFile(first, nil, 0o600))
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spare := shortTempDir(t)
			candidates := privateRoots(t)
			first := candidates[0]
			tc.setup(t, first, spare)
			before, err := os.Lstat(first)
			require.NoError(t, err)

			dir, err := PrivateSocketDir()
			if len(candidates) > 1 {
				require.NoError(t, err)
				require.Equal(t, candidates[1], dir, "the unsafe folder is skipped")
				requirePerm(t, dir, 0o700)
			} else {
				require.ErrorIs(t, err, ErrNoPrivateSocketDir)
				require.Empty(t, dir)
			}
			// The skipped folder is left as it was.
			after, err := os.Lstat(first)
			require.NoError(t, err)
			require.Equal(t, before.Mode(), after.Mode())

			// The reason reaches the error when nothing else is left.
			if len(candidates) > 1 {
				require.NoError(t, os.Remove(candidates[1]))
				require.NoError(t, os.WriteFile(candidates[1], nil, 0o600))
				_, err = PrivateSocketDir()
				require.ErrorIs(t, err, ErrNoPrivateSocketDir)
			}
			require.ErrorContains(t, err, first+": "+tc.reason)
		})
	}
}

func TestNoBareTmpFallback(t *testing.T) {
	short := shortTempDir(t)
	target := shortTempDir(t)
	long := filepath.Join(shortTempDir(t), strings.Repeat("d", 100))
	require.NoError(t, os.MkdirAll(long, 0o700))
	t.Setenv("XDG_RUNTIME_DIR", long)
	t.Setenv("TMPDIR", long)
	old := shortSocketBase
	shortSocketBase = short
	t.Cleanup(func() { shortSocketBase = old })

	// Every usual folder is too long for a socket path, so the folder is
	// /tmp/crush-<uid>, private like the others: never /tmp itself.
	want := filepath.Join(short, fmt.Sprintf("crush-%d", os.Getuid()))
	dir, err := PrivateSocketDir()
	require.NoError(t, err)
	require.Equal(t, want, dir)
	requirePerm(t, dir, 0o700)
	require.Equal(t, "unix://"+filepath.Join(want, "crush.sock"), DefaultHost())
	entries, err := os.ReadDir(short)
	require.NoError(t, err)
	require.Len(t, entries, 1, "nothing but the private folder lands in /tmp")

	// When that folder is not private either, there is no socket folder at
	// all, and no default host.
	require.NoError(t, os.Remove(want))
	require.NoError(t, os.Symlink(target, want))
	_, err = PrivateSocketDir()
	require.ErrorIs(t, err, ErrNoPrivateSocketDir)
	require.ErrorContains(t, err, "would be over 104 bytes")
	require.ErrorContains(t, err, want+": it is a symlink")
	require.Empty(t, DefaultHost())
}

func TestDefaultHostInPrivateDir(t *testing.T) {
	candidates := privateRoots(t)

	dir, err := PrivateSocketDir()
	require.NoError(t, err)
	require.Equal(t, candidates[0], dir)
	host := DefaultHost()
	require.Equal(t, "unix://"+filepath.Join(dir, "crush.sock"), host)
	require.NotContains(t, host, fmt.Sprintf("crush-%d.sock", os.Getuid()), "the old socket name")

	// With no private folder there is no default host to fall back to.
	old := socketDirOwner
	socketDirOwner = func(os.FileInfo) (int, bool) { return os.Getuid() + 1, true }
	t.Cleanup(func() { socketDirOwner = old })
	require.Empty(t, DefaultHost())
}

func TestEngineSocketNameStable(t *testing.T) {
	base := shortTempDir(t)
	privateRoots(t)
	real := filepath.Join(base, "real")
	require.NoError(t, os.Mkdir(real, 0o700))
	link := filepath.Join(base, "link")
	require.NoError(t, os.Symlink(real, link))
	dataDir := filepath.Join(real, ".crush")

	dir, err := PrivateSocketDir()
	require.NoError(t, err)
	first, err := EngineSocketPath(dataDir)
	require.NoError(t, err)
	require.Equal(t, dir, filepath.Dir(first))
	require.Regexp(t, `^engine-[0-9a-f]{16}\.sock$`, filepath.Base(first))

	same := func(path string) {
		t.Helper()
		got, err := EngineSocketPath(path)
		require.NoError(t, err)
		require.Equal(t, first, got, "socket for %s", path)
	}
	// The data folder does not exist yet.
	same(dataDir)
	same(real + "/./.crush/")
	same(filepath.Join(link, ".crush"))
	// Created now.
	require.NoError(t, os.Mkdir(dataDir, 0o700))
	same(dataDir)
	same(filepath.Join(link, ".crush"))
	require.NoError(t, os.Symlink(dataDir, filepath.Join(base, "data-link")))
	same(filepath.Join(base, "data-link"))

	other, err := EngineSocketPath(filepath.Join(base, "other", ".crush"))
	require.NoError(t, err)
	require.NotEqual(t, first, other)

	address, err := EngineAddress(dataDir)
	require.NoError(t, err)
	require.Equal(t, "unix://"+first, address)
}
