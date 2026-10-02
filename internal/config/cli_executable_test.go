package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFindCLIWindowsExtensions(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	for _, name := range []string{"codex.exe", "claude.cmd", "custom.tool", "plain"} {
		require.NoError(t, os.WriteFile(filepath.Join(second, name), nil, 0o600))
	}
	require.NoError(t, os.Mkdir(filepath.Join(first, "codex.exe"), 0o700))
	path := first + ";" + second
	for _, name := range []string{"codex", "claude", "codex.exe"} {
		ext := ".exe"
		if name == "claude" {
			ext = ".cmd"
		}
		want := filepath.Join(second, name)
		if filepath.Ext(name) == "" {
			want += ext
		}
		require.Equal(t, want, findCLIForPlatform(path, name, "windows", ".EXE;.CMD"))
	}
	require.Equal(t, filepath.Join(second, "custom.tool"), findCLIForPlatform(path, "custom", "windows", ".TOOL"))
	require.Equal(t, filepath.Join(second, "claude.cmd"), findCLIForPlatform(path, "claude", "windows", ""))
	require.Empty(t, findCLIForPlatform(path, "codex", "windows", ".CMD"))
	require.Empty(t, findCLIForPlatform(path, "plain", "windows", ".EXE;.CMD"))
	require.Empty(t, findCLIForPlatform("", "codex", "windows", ".EXE"))
}

func TestFindCLIUnixExecutablePermissions(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "codex")
	require.NoError(t, os.WriteFile(bin, nil, 0o600))
	require.Empty(t, findCLIForPlatform(dir, "codex", "linux", ".EXE"))
	require.NoError(t, os.Chmod(bin, 0o700))
	require.Equal(t, bin, findCLIForPlatform(dir, "codex", "linux", ".EXE"))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "claude"), 0o700))
	require.Empty(t, findCLIForPlatform(dir, "claude", "linux", ""))
}
