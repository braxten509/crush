package cliagent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/crush/internal/agent/tools"
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
