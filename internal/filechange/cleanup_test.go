package filechange

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTrackedDeletionBecomesOnlyAFlag(t *testing.T) {
	root := visibleRestoreRoot(t)
	for _, shared := range []bool{false, true} {
		tracker, err := New(t.Context(), root)
		require.NoError(t, err)
		if shared {
			tracker.store = newSnapshotStore()
			defer tracker.store.releaseAll()
		}
		path := filepath.Join(root, ".cache", "library.js")
		put(t, root, ".cache/library.js", "downloaded library")
		tracker.Track(path)
		require.NoError(t, os.Remove(path))
		review, err := tracker.Checkpoint(t.Context())
		require.NoError(t, err)
		require.Empty(t, review.Changes)
		require.True(t, review.Deletions)
	}
}

func TestPreviewBudgetSurvivesCheckpointAndDeletion(t *testing.T) {
	root := visibleRestoreRoot(t)
	tracker, err := New(t.Context(), root)
	require.NoError(t, err)
	for i := range maxTextTotal / maxTextSize {
		name := fmt.Sprint(i)
		put(t, root, name, strings.Repeat("x", maxTextSize))
		tracker.Track(name)
	}
	put(t, root, "overflow", "new content")
	tracker.Track("overflow")
	require.Empty(t, tracker.files[filepath.Join(root, "overflow")].state.Content)
	for path := range tracker.files {
		require.NoError(t, os.Remove(path))
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = tracker.Checkpoint(ctx)
	require.ErrorIs(t, err, context.Canceled)
	_, err = tracker.Checkpoint(t.Context())
	require.NoError(t, err)
	put(t, root, "next", "new preview")
	tracker.Track("next")
	put(t, root, "next", "changed preview")
	review, err := tracker.Checkpoint(t.Context())
	require.NoError(t, err)
	require.Len(t, review.Changes, 1)
	require.Equal(t, "new preview", review.Changes[0].Before.Content)
	require.Equal(t, "changed preview", review.Changes[0].After.Content)
}

func TestHiddenAliasStillCapturesVisibleSource(t *testing.T) {
	root := visibleRestoreRoot(t)
	put(t, root, "source.txt", "original source")
	require.NoError(t, os.Mkdir(filepath.Join(root, ".cache"), 0700))
	alias := filepath.Join(root, ".cache", "alias")
	require.NoError(t, os.Symlink(filepath.Join(root, "source.txt"), alias))
	tracker, err := New(t.Context(), root)
	require.NoError(t, err)
	tracker.Track(alias)
	require.NoError(t, os.WriteFile(alias, []byte("changed source"), 0600))
	review, err := tracker.Checkpoint(t.Context())
	require.NoError(t, err)
	changes := byPath(review)
	require.Equal(t, "original source", changes["source.txt"].Before.Content)
	require.Equal(t, "changed source", changes["source.txt"].After.Content)
}
