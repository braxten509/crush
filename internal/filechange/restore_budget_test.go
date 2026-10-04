package filechange

import (
	"encoding/base64"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func visibleRestoreRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp(".", "restore-budget-")
	require.NoError(t, err)
	root, err = filepath.Abs(root)
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(root) })
	return root
}
func TestCheckpointHandsOffAndDropsRestorePayload(t *testing.T) {
	root := visibleRestoreRoot(t)
	tracker, err := New(t.Context(), root)
	require.NoError(t, err)
	tracker.Track("large.bin")
	put(t, root, "large.bin", strings.Repeat("x", maxTextSize+1))
	review, err := tracker.Checkpoint(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, review.Changes[0].After.RestoreData)
	value := tracker.files[filepath.Join(root, "large.bin")].state
	require.Empty(t, value.RestoreData)
	require.Empty(t, value.Content)
	require.True(t, value.RestoreDigestOnly)
	unchanged, err := tracker.Checkpoint(t.Context())
	require.NoError(t, err)
	require.Empty(t, unchanged.Changes)
	put(t, root, "large.bin", "new")
	changed, err := tracker.Checkpoint(t.Context())
	require.NoError(t, err)
	require.Equal(t, value.Digest, changed.Changes[0].Before.Digest)
	require.True(t, changed.Changes[0].Before.RestoreDigestOnly)
}
func TestCheckpointBudgetMarksRemainder(t *testing.T) {
	root := visibleRestoreRoot(t)
	tracker, err := New(t.Context(), root)
	require.NoError(t, err)
	tracker.restoreRemaining = 12
	for _, name := range []string{"a.bin", "b.bin", "c.bin"} {
		tracker.Track(name)
		put(t, root, name, "12345678")
	}
	review, err := tracker.Checkpoint(t.Context())
	require.NoError(t, err)
	total, omitted := 0, 0
	for _, change := range review.Changes {
		data, err := base64.StdEncoding.DecodeString(change.After.RestoreData)
		require.NoError(t, err)
		total += len(data)
		if change.After.RestoreOmitted == RestoreBudgetReason {
			omitted++
		}
	}
	require.LessOrEqual(t, total, 12)
	require.Equal(t, 2, omitted)
}
func TestImportedAndGeneratedFilesRemainStatOnlyUntilEdited(t *testing.T) {
	for _, imported := range []bool{true, false} {
		t.Run(map[bool]string{true: "checkout", false: "artifact"}[imported], func(t *testing.T) {
			root := visibleRestoreRoot(t)
			tracker, err := New(t.Context(), root)
			require.NoError(t, err)
			tracker.imported = imported
			path := "build/output.jar"
			tracker.Track(path)
			put(t, root, path, "copied bytes")
			review, err := tracker.Checkpoint(t.Context())
			require.NoError(t, err)
			require.Empty(t, review.Changes[0].After.Content)
			require.Empty(t, review.Changes[0].After.RestoreData)
			require.True(t, strings.HasPrefix(review.Changes[0].After.Digest, "stat:"))
			require.Equal(t, MaxRestoreTotal, tracker.restoreRemaining)
			tracker.Track(path) // a later edit explicitly asks for the baseline
			require.NotNil(t, tracker.files[filepath.Join(root, path)].restore)
			require.Empty(t, tracker.files[filepath.Join(root, path)].state.RestoreData)
			put(t, root, path, "edited bytes")
			review, err = tracker.Checkpoint(t.Context())
			require.NoError(t, err)
			require.Empty(t, review.Changes[0].Before.RestoreOmitted)
			require.NotEmpty(t, review.Changes[0].Before.RestoreData)
		})
	}
}

func TestUnchangedBaselineSpillsUntilFirstEdit(t *testing.T) {
	root := visibleRestoreRoot(t)
	original := strings.Repeat("x", maxTextSize+1)
	put(t, root, "file", original)
	tracker, err := New(t.Context(), root)
	require.NoError(t, err)
	tracker.Track("file")
	require.Empty(t, tracker.files[filepath.Join(root, "file")].state.RestoreData)
	require.NotNil(t, tracker.files[filepath.Join(root, "file")].restore)
	review, err := tracker.Checkpoint(t.Context())
	require.NoError(t, err)
	require.Empty(t, review.Changes)
	put(t, root, "file", "edited")
	review, err = tracker.Checkpoint(t.Context())
	require.NoError(t, err)
	data, err := base64.StdEncoding.DecodeString(review.Changes[0].Before.RestoreData)
	require.NoError(t, err)
	require.Equal(t, original, string(data))
}

func TestCommandFinalizationDoesNotReadNewBuildArtifact(t *testing.T) {
	root := visibleRestoreRoot(t)
	ctx, report := WithCommandReview(t.Context(), root)
	path := filepath.Join(root, "build", "output.jar")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	BeforeOpen(ctx, path, os.O_WRONLY|os.O_CREATE)
	require.NoError(t, os.WriteFile(path, []byte("build bytes"), 0644))
	review := report.Finish()
	require.Len(t, review.Changes, 1)
	require.Empty(t, review.Changes[0].After.RestoreData)
	require.Empty(t, review.Changes[0].After.Content)
	require.Equal(t, "Generated build artifact", review.Changes[0].After.Omitted)
	require.Equal(t, MaxRestoreTotal, report.store.restoreRemaining)
}
