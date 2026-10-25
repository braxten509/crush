package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKimiUnusable(t *testing.T) {
	t.Parallel()

	// gitInit makes dir look like a real Git project.
	gitInit := func(t *testing.T, dir string) {
		t.Helper()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644))
	}
	project := func(t *testing.T, home string) string {
		t.Helper()
		dir := filepath.Join(home, "dev", "project")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		gitInit(t, dir)
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
				gitInit(t, home)
				return home, home
			},
			want: kimiHomeMessage,
		},
		{
			name:     "project whose git root is home",
			provider: KimiProviderID,
			setup: func(t *testing.T) (string, string) {
				home := t.TempDir()
				gitInit(t, home)
				dir := filepath.Join(home, "dev", "project")
				require.NoError(t, os.MkdirAll(dir, 0o755))
				return dir, home
			},
			want: kimiHomeProjectMessage,
		},
		{
			// An empty ~/.git is not a project, so ~/dev is just a
			// plain folder, not the home folder.
			name:     "folder under a home with an empty .git",
			provider: KimiProviderID,
			setup: func(t *testing.T) (string, string) {
				home := t.TempDir()
				require.NoError(t, os.Mkdir(filepath.Join(home, ".git"), 0o755))
				dir := filepath.Join(home, "dev")
				require.NoError(t, os.MkdirAll(dir, 0o755))
				return dir, home
			},
			want: kimiNoGitMessage,
		},
		{
			name:     "worktree with a .git file",
			provider: KimiProviderID,
			setup: func(t *testing.T) (string, string) {
				home := t.TempDir()
				dir := filepath.Join(home, "wt")
				require.NoError(t, os.MkdirAll(dir, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: /elsewhere\n"), 0o644))
				return dir, home
			},
			want: "",
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
