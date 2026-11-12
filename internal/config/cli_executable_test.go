package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

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
