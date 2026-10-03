package secureentry

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func template(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "credentials.env")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
	return path
}

func TestSequentialEntriesStayLocal(t *testing.T) {
	path := template(t, "FIRST=%s\nSECOND=%s\n")
	for _, value := range []string{"dummy-one-$HOME-`cmd`", "dummy-two"} {
		target, err := Prepare(Spec{File: path, Occurrence: 1})
		require.NoError(t, err)
		require.NoError(t, target.Save([]byte(value)))
		require.NotContains(t, fmt.Sprintf("%+v", target), value)
		target.Close()
		require.ErrorIs(t, target.Save([]byte(value)), ErrClosed)
	}
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "FIRST=dummy-one-$HOME-`cmd`\nSECOND=dummy-two\n", string(data))
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestSaveRefusesChangesAndSymlinks(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"edit", "replacement", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			path := template(t, "KEY=%s\n")
			target, err := Prepare(Spec{File: path, Occurrence: 1})
			require.NoError(t, err)
			defer target.Close()
			switch kind {
			case "edit":
				require.NoError(t, os.WriteFile(path, []byte("changed locally"), 0o600))
			case "replacement":
				replacement := filepath.Join(filepath.Dir(path), "replacement")
				require.NoError(t, os.WriteFile(replacement, []byte("KEY=%s\n"), 0o600))
				require.NoError(t, os.Rename(replacement, path))
			case "symlink":
				other := template(t, "KEY=%s\n")
				require.NoError(t, os.Remove(path))
				require.NoError(t, os.Symlink(other, path))
			}
			err = target.Save([]byte("dummy-never-written"))
			require.Error(t, err)
			data, readErr := os.ReadFile(path)
			require.NoError(t, readErr)
			require.NotContains(t, string(data), "dummy-never-written")
		})
	}
}

func TestPlaceholderSelectionAndLiteralReplacement(t *testing.T) {
	t.Parallel()
	path := template(t, "one=%s two=%s three=TOKEN_SLOT")
	target, err := Prepare(Spec{File: path, Occurrence: 2})
	require.NoError(t, err)
	defer target.Close()
	require.NoError(t, target.Save([]byte("literal-%s-$1")))
	next, err := Prepare(Spec{File: path, Placeholder: "TOKEN_SLOT", Occurrence: 1})
	require.NoError(t, err)
	defer next.Close()
	require.NoError(t, next.Save([]byte("third-key")))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "one=%s two=literal-%s-$1 three=third-key", string(data))
}

func TestValidationDoesNotRevealContents(t *testing.T) {
	t.Parallel()
	path := template(t, "EXISTING=dummy-private-key\nNEXT=%s")
	for _, spec := range []Spec{{File: path, Occurrence: 0}, {File: path, Occurrence: 2}, {File: path, Occurrence: 1, Placeholder: "missing"}, {File: path, Occurrence: 1, Label: "bad\x1b[0m"}, {File: "relative", Occurrence: 1}} {
		_, err := Prepare(spec)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "dummy-private-key")
	}
	target, err := Prepare(Spec{File: path, Occurrence: 1})
	require.NoError(t, err)
	defer target.Close()
	for _, value := range []string{"", "dummy\nkey", "dummy\x00key", strings.Repeat("x", (64<<10)+1)} {
		require.Error(t, target.Save([]byte(value)))
	}
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "EXISTING=dummy-private-key\nNEXT=%s", string(data))
}
