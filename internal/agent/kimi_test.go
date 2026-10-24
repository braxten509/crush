package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKimiUnusable(t *testing.T) {
	t.Parallel()

	project := func(t *testing.T, home string) string {
		t.Helper()
		dir := filepath.Join(home, "dev", "project")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.Mkdir(filepath.Join(dir, ".git"), 0o755))
		return dir
	}

	tests := []struct {
		name     string
		provider string
		setup    func(t *testing.T) (dir, home string)
		want     string
	}{
		{
			name:     "git project",
			provider: KimiProviderID,
			setup: func(t *testing.T) (string, string) {
				home := t.TempDir()
				return project(t, home), home
			},
			want: "",
		},
		{
			name:     "subfolder of a git project",
			provider: KimiProviderID,
			setup: func(t *testing.T) (string, string) {
				home := t.TempDir()
				dir := filepath.Join(project(t, home), "pkg")
				require.NoError(t, os.MkdirAll(dir, 0o755))
				return dir, home
			},
			want: "",
		},
		{
			name:     "plain folder",
			provider: KimiProviderID,
			setup: func(t *testing.T) (string, string) {
				home := t.TempDir()
				dir := filepath.Join(home, "notes")
				require.NoError(t, os.MkdirAll(dir, 0o755))
				return dir, home
			},
			want: kimiNoGitMessage,
		},
		{
			name:     "home folder with a .git",
			provider: KimiProviderID,
			setup: func(t *testing.T) (string, string) {
				home := t.TempDir()
				require.NoError(t, os.Mkdir(filepath.Join(home, ".git"), 0o755))
				return home, home
			},
			want: kimiHomeMessage,
		},
		{
			name:     "project whose git root is home",
			provider: KimiProviderID,
			setup: func(t *testing.T) (string, string) {
				home := t.TempDir()
				require.NoError(t, os.Mkdir(filepath.Join(home, ".git"), 0o755))
				dir := filepath.Join(home, "dev", "project")
				require.NoError(t, os.MkdirAll(dir, 0o755))
				return dir, home
			},
			want: kimiHomeMessage,
		},
		{
			name:     "other provider in a plain folder",
			provider: "openai",
			setup: func(t *testing.T) (string, string) {
				home := t.TempDir()
				return filepath.Join(home, "notes"), home
			},
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir, home := tc.setup(t)
			require.Equal(t, tc.want, kimiUnusableAt(tc.provider, dir, home))
		})
	}
}
