package filechange

import (
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestRepositoryScopeIncludesSiblingAndNewFilesButNotExternalAliases(t *testing.T) {
	root := visibleRestoreRoot(t)
	require.NoError(t, os.Mkdir(filepath.Join(root, ".git"), 0700))
	require.NoError(t, os.Mkdir(filepath.Join(root, "subdir"), 0700))
	outside := visibleRestoreRoot(t)
	put(t, root, "sibling.txt", "before")
	put(t, outside, "external.txt", "private")
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "external")))
	tracker, err := New(t.Context(), filepath.Join(root, "subdir"))
	require.NoError(t, err)
	tracker.Track("../sibling.txt")
	tracker.Track("new.txt")
	tracker.Track(filepath.Join(outside, "external.txt"))
	tracker.Track("../external/external.txt")
	put(t, root, "sibling.txt", "after")
	put(t, root, "subdir/new.txt", "new source")
	put(t, outside, "external.txt", "changed private")
	review, err := tracker.Checkpoint(t.Context())
	require.NoError(t, err)
	changes := byPath(review)
	require.Len(t, changes, 2)
	require.Equal(t, "before", changes["sibling.txt"].Before.Content)
	require.Equal(t, "new source", changes["new.txt"].After.Content)
	require.NotContains(t, changes, "external.txt")
}

func TestNoRepositoryRecordsNoFiles(t *testing.T) {
	root := t.TempDir()
	require.Empty(t, RepositoryRoot(root))
	put(t, root, "file.txt", "before")
	tracker, err := New(t.Context(), root)
	require.NoError(t, err)
	tracker.Track("file.txt")
	put(t, root, "file.txt", "after")
	review, err := tracker.Checkpoint(t.Context())
	require.NoError(t, err)
	require.Empty(t, review.Changes)
}

func TestLinkedWorktreeScope(t *testing.T) {
	root := t.TempDir()
	put(t, root, ".git", "gitdir: /a/separate/worktree\n")
	require.NoError(t, os.Mkdir(filepath.Join(root, "nested"), 0700))
	require.Equal(t, root, RepositoryRoot(filepath.Join(root, "nested")))
}

func TestReportedDeletionIsFlagOnlyEvenWithoutRepository(t *testing.T) {
	root := t.TempDir()
	review := ScopeReview(&Review{Root: root, Changes: []Change{
		{Path: filepath.Join(root, "gone"), Before: &State{Content: "removed"}},
		{Path: filepath.Join(root, "external"), After: &State{Content: "private"}},
	}}, root)
	require.True(t, review.Deletions)
	require.Empty(t, review.Changes)
}

func TestOutsideWritesDoNotAccumulateCapturePaths(t *testing.T) {
	root := visibleRestoreRoot(t)
	require.NoError(t, os.Mkdir(filepath.Join(root, ".git"), 0700))
	outside := t.TempDir()
	process := newProcessReview(root, nil)
	record := process.executed(nil, []string{"writer"})
	ctx, report := WithCommandReview(t.Context(), root)
	for _, name := range []string{"one", "two", "three"} {
		path := filepath.Join(outside, name)
		process.before(record, path)
		BeforeOpen(ctx, path, os.O_CREATE|os.O_WRONLY)
	}
	require.Empty(t, record.captured)
	require.Empty(t, process.invocations)
	require.Empty(t, report.directAt)
	require.Nil(t, report.Finish())
}
