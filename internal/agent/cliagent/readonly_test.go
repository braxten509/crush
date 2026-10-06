package cliagent

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

func TestWithin(t *testing.T) {
	t.Parallel()
	require.True(t, within("/a/b", "/a"))
	require.True(t, within("/a", "/a"))
	require.False(t, within("/ab", "/a"))
	require.False(t, within("/", "/a"))
}

// A read-only sub-agent's commands can't change the project, while the
// CLI's own state and the cache stay writable.
func TestReadOnlyCommandKeepsProjectUnchanged(t *testing.T) {
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skip("codex isn't installed")
	}
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	// The sandbox always lets programs write in the temporary folder, so
	// the project must live elsewhere.
	project, err := os.MkdirTemp(home, ".crush-readonly-test-")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(project) })
	cache, err := os.MkdirTemp(filepath.Join(home, ".cache"), "crush-readonly-test-")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(cache) })

	m := &Model{Kind: config.TypeClaudeCode, Dir: project, ReadOnly: true}
	dir, name, args, err := m.readOnlyCommand("/bin/sh", []string{"-c", `pwd; touch project-file; touch "$CACHE/cache-file"`})
	require.NoError(t, err)
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CACHE="+cache)
	out, _ := cmd.CombinedOutput()
	require.Contains(t, string(out), project, "the CLI runs in the project")
	require.NoFileExists(t, filepath.Join(project, "project-file"), string(out))
	require.FileExists(t, filepath.Join(cache, "cache-file"), string(out))

	_, _, _, err = (&Model{Kind: config.TypeClaudeCode, Dir: t.TempDir(), ReadOnly: true}).readOnlyCommand("claude", nil)
	require.Error(t, err, "a project in the temporary folder can't be kept read-only")
}
