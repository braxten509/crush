package diffreview

import (
	"testing"

	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/stretchr/testify/require"
)

func TestImportedBaselineCountsOnlyLaterEdits(t *testing.T) {
	t.Parallel()
	base := &filechange.State{Content: "one\ntwo\nthree\n", Mode: 0o644}
	edited := &filechange.State{Content: "one\nchanged\nthree\n", Mode: 0o644}
	imported := filechange.Change{Path: "checkout/file", Order: 1, After: base,
		Transfer: &filechange.Transfer{Kind: "checkout", Baseline: base}}
	files := Build([]Edit{{Path: imported.Path, Snapshot: &imported}})
	require.Len(t, files, 1)
	require.Equal(t, Copied, files[0].Kind)
	require.Equal(t, 0, files[0].Adds)
	require.False(t, files[0].HasEdits())
	require.Len(t, files[0].Lines, 1)
	require.Equal(t, "copied checkout: 1 file", Summary(files))
	edit := filechange.Change{Path: imported.Path, Order: 2, Before: base, After: edited}
	files = Build([]Edit{{Path: edit.Path, Snapshot: &edit}, {Path: imported.Path, Snapshot: &imported}})
	require.Equal(t, 1, files[0].Adds)
	require.Equal(t, 1, files[0].Dels)
	require.Equal(t, "copied checkout: 1 file · 1 file edited", Summary(files))
}

func TestMovedFilePreservesEditsWithoutCountingSourceDeletion(t *testing.T) {
	t.Parallel()
	base := &filechange.State{Content: "before\n", Mode: 0o644}
	after := &filechange.State{Content: "after\n", Mode: 0o644}
	source := filechange.Change{Path: "source", Before: base}
	destination := filechange.Change{Path: "destination", After: after,
		Transfer: &filechange.Transfer{Kind: "move", Source: "source", Baseline: base}}
	files := Build([]Edit{{Path: source.Path, Snapshot: &source}, {Path: destination.Path, Snapshot: &destination}})
	require.Len(t, files, 1)
	require.Equal(t, Moved, files[0].Kind)
	require.Equal(t, 1, files[0].Adds)
	require.Equal(t, 1, files[0].Dels)
}
