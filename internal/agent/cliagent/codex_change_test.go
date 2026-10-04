package cliagent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/stretchr/testify/require"
)

func TestReversePatchRestoresEveryHunk(t *testing.T) {
	t.Parallel()
	patched := "a\nB\nc\nd\ne\nf\nG\nh\n"
	patch := "@@ -1,3 +1,3 @@\n a\n-b\n+B\n c\n@@ -6,3 +6,3 @@\n f\n-g\n+G\n h\n"
	before, ok := reversePatch(patched, patch)
	require.True(t, ok)
	require.Equal(t, "a\nb\nc\nd\ne\nf\ng\nh\n", before)
	_, ok = reversePatch("unrelated\n", patch)
	require.False(t, ok, "a patch that no longer matches must not invent a before")
}

func TestCodexChangeMetadataDoesNotDependOnReadTiming(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\ny\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "old.txt"), []byte("1\nTWO\n3\n"), 0o644))
	var meta tools.EditResponseMetadata

	add := codexChange{Path: "new.txt", Diff: "x\ny\n"}
	add.Kind.Type = "add"
	require.NoError(t, json.Unmarshal([]byte(codexChangeMetadata(dir, add)), &meta))
	require.Equal(t, tools.EditResponseMetadata{Additions: 2, NewContent: "x\ny\n"}, meta)

	update := codexChange{Path: filepath.Join(dir, "old.txt"), Diff: "@@ -1,3 +1,3 @@\n 1\n-2\n+TWO\n 3\n"}
	update.Kind.Type = "update"
	meta = tools.EditResponseMetadata{}
	require.NoError(t, json.Unmarshal([]byte(codexChangeMetadata(dir, update)), &meta))
	require.Equal(t, tools.EditResponseMetadata{Additions: 1, Removals: 1, OldContent: "1\n2\n3\n", NewContent: "1\nTWO\n3\n"}, meta)
}

func TestCodexReversedPatchCarriesFullRestoreStates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file")
	require.NoError(t, os.WriteFile(path, []byte("one\nCHANGED\nthree\n"), 0750))
	change := codexChange{Path: path, Diff: "@@ -1,3 +1,3 @@\n one\n-two\n+CHANGED\n three\n"}
	change.Kind.Type = "update"
	_, review := filechange.TakeReview(codexChangeMetadata(dir, change))
	require.NotNil(t, review)
	require.Len(t, review.Changes, 1)
	require.Equal(t, "one\ntwo\nthree\n", review.Changes[0].Before.Content)
	require.Equal(t, "one\nCHANGED\nthree\n", review.Changes[0].After.Content)
	require.EqualValues(t, 0750, review.Changes[0].Before.Mode)
	change.Kind.Type = "add"
	_, review = filechange.TakeReview(codexChangeMetadata(dir, change))
	require.Nil(t, review.Changes[0].Before)
}

func TestReversePatchUsesPositionsAndFinalNewline(t *testing.T) {
	before, ok := reversePatch("same\nsame\n", "@@ -2 +2 @@\n-old\n+same\n")
	require.True(t, ok)
	require.Equal(t, "same\nold\n", before)
	before, ok = reversePatch("new\n", "@@ -1 +1 @@\n-old\n\\ No newline at end of file\n+new\n")
	require.True(t, ok)
	require.Equal(t, "old", before)
	before, ok = reversePatch("new", "@@ -1 +1 @@\n-old\n+new\n\\ No newline at end of file\n")
	require.True(t, ok)
	require.Equal(t, "old\n", before)
	before, ok = reversePatch("first\nthird\n", "@@ -2 +1,0 @@\n-second\n")
	require.True(t, ok)
	require.Equal(t, "first\nsecond\nthird\n", before)
}
