package filechange

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/crush/internal/secureentry"
	"github.com/stretchr/testify/require"
)

func TestSnapshotsExcludeLateSecureEntry(t *testing.T) {
	t.Parallel()
	root := visibleRestoreRoot(t)
	path := filepath.Join(root, "credentials.env")
	require.NoError(t, os.WriteFile(path, []byte("KEY=%s"), 0o600))
	tracker, err := New(t.Context(), root)
	require.NoError(t, err)
	tracker.Track(path)
	ctx, command := WithCommandReview(context.Background(), root)
	BeforeOpen(ctx, path, os.O_WRONLY)
	// Include already captured process changes as well as shell redirects.
	command.changes = []Change{{Path: path, Before: &State{Content: "KEY=%s"}}}
	target, err := secureentry.Prepare(secureentry.Spec{File: path, Occurrence: 1})
	require.NoError(t, err)
	defer target.Close()
	require.NoError(t, target.Save([]byte("synthetic-secret")))
	review, err := tracker.Checkpoint(t.Context())
	require.NoError(t, err)
	require.Empty(t, review.Changes)
	require.Empty(t, tracker.files)
	require.False(t, tracker.Contains(path))
	final := command.Finish()
	require.NotNil(t, final)
	require.Len(t, final.Changes, 1)
	require.Empty(t, final.Changes[0].After.Content)
	require.Equal(t, "Secure entry destination", final.Changes[0].After.Omitted)
}
