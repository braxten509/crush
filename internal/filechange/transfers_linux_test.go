//go:build linux && amd64

package filechange

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestObservedCloneAndFollowingEdit(t *testing.T) {
	root := t.TempDir()
	put(t, root, "source/file.txt", "original\n")
	source := filepath.Join(root, "source")
	for _, args := range [][]string{
		{"init", source},
		{"-C", source, "add", "file.txt"},
		{"-C", source, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "-m", "fixture"},
	} {
		out, err := exec.Command("git", args...).CombinedOutput()
		if strings.Contains(string(out), "Codex Git guard: blocked") {
			t.Skip("Git guard prevented creation of the clone fixture")
		}
		require.NoError(t, err, "%s", out)
	}
	review, err := runObserved(t, root, `git clone source destination && printf 'edited\n' > destination/file.txt`)
	require.NoError(t, err)
	require.NotNil(t, review)
	require.Len(t, review.Changes, 1)
	change := review.Changes[0]
	require.NotNil(t, change.Transfer)
	require.Equal(t, "checkout", change.Transfer.Kind)
	require.Equal(t, "original\n", change.Transfer.Baseline.Content)
	require.Equal(t, "edited\n", change.After.Content)
}

func TestObservedBulkCopyAndFollowingEdit(t *testing.T) {
	root := t.TempDir()
	for index := range 12 {
		put(t, root, fmt.Sprintf("source/%d.txt", index), "original\n")
	}
	review, err := runObserved(t, root, `cp -r source destination && printf 'edited\n' > destination/0.txt`)
	require.NoError(t, err)
	require.NotNil(t, review)
	require.Len(t, review.Changes, 12)
	for _, change := range review.Changes {
		require.NotNil(t, change.Transfer)
		require.Equal(t, "copy", change.Transfer.Kind)
		if filepath.Base(change.Path) == "0.txt" {
			require.Equal(t, "original\n", change.ImportedState().Content)
		} else {
			require.Empty(t, change.After.Content, "unmodified imported contents are not materialized")
		}
	}
}

func TestObservedRenameAndFollowingEdit(t *testing.T) {
	root := t.TempDir()
	put(t, root, "source", "original\n")
	review, err := runObserved(t, root, `python3 - <<'PY'
from pathlib import Path
Path('source').rename('destination')
Path('destination').write_text('edited\n')
PY`)
	require.NoError(t, err)
	require.NotNil(t, review)
	for _, change := range review.Changes {
		if filepath.Base(change.Path) != "destination" {
			continue
		}
		require.NotNil(t, change.Transfer)
		require.Equal(t, "move", change.Transfer.Kind)
		require.Equal(t, "original\n", change.Transfer.Baseline.Content)
		require.Equal(t, "edited\n", change.After.Content)
		return
	}
	t.Fatal("missing destination change")
}
