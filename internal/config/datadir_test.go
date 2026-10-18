package config

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestResolveDataDirMatchesLoad proves the resolver picks the data folder
// Load picks, case by case: the flag, a relative and an absolute config
// option, a .crush at the worktree root seen from a subfolder, and no
// .crush at all (engine-design.md 5.2).
func TestResolveDataDirMatchesLoad(t *testing.T) {
	t.Setenv("CRUSH_DISABLE_PROVIDER_AUTO_UPDATE", "1")

	writeConfig := func(t *testing.T, dir, body string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "crush.json"), []byte(body), 0o644))
	}

	cases := []struct {
		name string
		// setup returns the working folder and the --data-dir flag.
		setup func(t *testing.T) (workingDir, flag string)
		want  func(workingDir string) string
	}{
		{
			name: "relative flag",
			setup: func(t *testing.T) (string, string) {
				return t.TempDir(), "custom-data"
			},
			want: func(wd string) string { return filepath.Join(wd, "custom-data") },
		},
		{
			name: "absolute flag over the config option",
			setup: func(t *testing.T) (string, string) {
				wd := t.TempDir()
				writeConfig(t, wd, `{"options":{"data_directory":"from-config"}}`)
				return wd, filepath.Join(t.TempDir(), "flag-data")
			},
		},
		{
			name: "relative config option",
			setup: func(t *testing.T) (string, string) {
				wd := t.TempDir()
				writeConfig(t, wd, `{"options":{"data_directory":"rel/data"}}`)
				return wd, ""
			},
			want: func(wd string) string { return filepath.Join(wd, "rel", "data") },
		},
		{
			name: "absolute config option",
			setup: func(t *testing.T) (string, string) {
				wd := t.TempDir()
				abs := filepath.Join(t.TempDir(), "abs-data")
				writeConfig(t, wd, `{"options":{"data_directory":`+jsonString(abs)+`}}`)
				return wd, ""
			},
		},
		{
			name: "dot crush at the worktree root from a subfolder",
			setup: func(t *testing.T) (string, string) {
				if _, err := exec.LookPath("git"); err != nil {
					t.Skip("git not available")
				}
				root := t.TempDir()
				gitInit := exec.CommandContext(t.Context(), "git", "init", "-q")
				gitInit.Dir = root
				require.NoError(t, gitInit.Run())
				require.NoError(t, os.Mkdir(filepath.Join(root, ".crush"), 0o700))
				sub := filepath.Join(root, "a", "b")
				require.NoError(t, os.MkdirAll(sub, 0o755))
				return sub, ""
			},
			want: func(wd string) string { return filepath.Join(filepath.Dir(filepath.Dir(wd)), ".crush") },
		},
		{
			name: "no dot crush",
			setup: func(t *testing.T) (string, string) {
				return t.TempDir(), ""
			},
			want: func(wd string) string { return filepath.Join(wd, ".crush") },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wd, flag := tc.setup(t)
			got, err := ResolveDataDir(wd, flag)
			require.NoError(t, err)

			store, err := Load(wd, flag, false)
			require.NoError(t, err)
			require.Equal(t, store.Config().Options.DataDirectory, got, "the resolver must match Load")
			if tc.want != nil {
				require.Equal(t, tc.want(wd), got)
			}
		})
	}
}

// TestCanonicalDataDirStableBeforeCreation proves a data folder that does
// not exist yet, under a symlinked parent, gets the same canonical name as
// through the real parent, before and after it is created.
func TestCanonicalDataDirStableBeforeCreation(t *testing.T) {
	t.Parallel()
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(real, link))

	viaReal, err := CanonicalDataDir(filepath.Join(real, "proj", ".crush"))
	require.NoError(t, err)
	viaLink, err := CanonicalDataDir(filepath.Join(link, "proj", ".crush"))
	require.NoError(t, err)
	require.Equal(t, viaReal, viaLink)

	require.NoError(t, os.MkdirAll(filepath.Join(real, "proj", ".crush"), 0o700))
	after, err := CanonicalDataDir(filepath.Join(link, "proj", ".crush"))
	require.NoError(t, err)
	require.Equal(t, viaReal, after)
}

// jsonString quotes s as a JSON string.
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
