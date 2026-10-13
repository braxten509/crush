package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/crush/internal/session"
	"github.com/stretchr/testify/require"
)

func TestSessionSetupFromSubdirectoryUsesProjectDataDir(t *testing.T) {
	t.Setenv("CRUSH_GLOBAL_CONFIG", t.TempDir())
	t.Setenv("CRUSH_GLOBAL_DATA", t.TempDir())
	t.Setenv("CRUSH_DISABLE_PROVIDER_AUTO_UPDATE", "1")

	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	gitInit := exec.CommandContext(t.Context(), "git", "init", "-q")
	gitInit.Dir = root
	require.NoError(t, gitInit.Run())
	require.NoError(t, os.Mkdir(filepath.Join(root, ".crush"), 0o700))
	sub := filepath.Join(root, "sub")
	require.NoError(t, os.Mkdir(sub, 0o755))
	t.Chdir(sub)

	sessionListCmd.SetContext(t.Context())
	_, svc, cleanup, err := sessionSetup(sessionListCmd)
	require.NoError(t, err)
	defer cleanup()

	require.Equal(t, filepath.Join(root, ".crush"), svc.cfg.Config().Options.DataDirectory)
	require.NoDirExists(t, filepath.Join(sub, ".crush"))
}

func TestSessionsMatchingHash(t *testing.T) {
	t.Parallel()

	ids := []string{"11111111-aaaa", "22222222-bbbb", "33333333-cccc"}
	idOf := func(s string) string { return s }
	full := session.HashID(ids[1])

	require.Equal(t, []string{ids[1]}, sessionsMatchingHash(ids, full, idOf))
	require.Equal(t, []string{ids[1]}, sessionsMatchingHash(ids, full[:7], idOf))
	require.Empty(t, sessionsMatchingHash(ids, "not-a-hash", idOf))
	require.Len(t, sessionsMatchingHash(ids, "", idOf), len(ids))
}
