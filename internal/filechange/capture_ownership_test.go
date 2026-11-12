package filechange

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func restoredBytes(t *testing.T, state *State) string {
	t.Helper()
	require.NotNil(t, state)
	require.Empty(t, state.RestoreOmitted)
	data, err := base64.StdEncoding.DecodeString(state.RestoreData)
	require.NoError(t, err)
	return string(data)
}

func TestReviewSharedBaselineReleasedAfterLastOwner(t *testing.T) {
	root := visibleRestoreRoot(t)
	p := newProcessReview(root, nil)
	p.store.restoreRemaining = 12
	path := filepath.Join(root, "shared")
	require.NoError(t, os.WriteFile(path, []byte("original"), 0o600))
	for _, id := range []string{"one", "two"} {
		p.Begin(id, id)
		record := p.executed(nil, []string{id})
		p.before(record, path)
	}
	require.Nil(t, p.End("one"))
	require.NoError(t, os.WriteFile(path, []byte("edit"), 0o600))
	review := p.End("two")
	require.Equal(t, "original", restoredBytes(t, review.Changes[0].Before))
	require.Equal(t, "edit", restoredBytes(t, review.Changes[0].After))
	require.Equal(t, 12, p.store.restoreRemaining)
}

func TestNativeFinishReusesCapturesAndReturnsSameReview(t *testing.T) {
	for _, subprocess := range []bool{false, true} {
		name := "redirect"
		if subprocess {
			name = "subprocess"
		}
		t.Run(name, func(t *testing.T) {
			root := visibleRestoreRoot(t)
			ctx, report := WithCommandReview(t.Context(), root)
			report.store.restoreRemaining = 12
			path := filepath.Join(root, "file")
			require.NoError(t, os.WriteFile(path, []byte("original"), 0o600))
			var process *ProcessReview
			if subprocess {
				process = newProcessReview(root, nil)
				process.store, process.report = report.store, report
				process.Begin("command", "writer")
				record := process.executed(nil, []string{"writer"})
				process.before(record, path)
			} else {
				BeforeOpen(ctx, path, os.O_WRONLY|os.O_TRUNC)
			}
			require.NoError(t, os.WriteFile(path, []byte("edit"), 0o600))
			if subprocess {
				report.changes = append(report.changes, process.End("command").Changes...)
			}
			review := report.Finish()
			require.Len(t, review.Changes, 1)
			require.Equal(t, "original", restoredBytes(t, review.Changes[0].Before))
			require.Equal(t, "edit", restoredBytes(t, review.Changes[0].After))
			require.Equal(t, 12, report.store.restoreRemaining)
			require.Empty(t, report.store.captures)
			require.NoError(t, os.WriteFile(path, []byte("later"), 0o600))
			require.Same(t, review, report.Finish())
			require.Equal(t, "edit", restoredBytes(t, report.Finish().Changes[0].After))
		})
	}
}

func TestNativeImportedBaselineSurvivesCaptureHandoff(t *testing.T) {
	root := visibleRestoreRoot(t)
	_, report := WithCommandReview(t.Context(), root)
	report.store.restoreRemaining = 12
	p := newProcessReview(root, nil)
	p.store, p.report = report.store, report
	p.Begin("command", "wrapper")
	wrapper := p.executed(nil, []string{"wrapper"})
	copy := p.executed(wrapper, []string{"cp", "source", "file"})
	path := filepath.Join(root, "file")
	p.before(copy, path)
	require.NoError(t, os.WriteFile(path, []byte("original"), 0o600))
	p.before(wrapper, path)
	require.NoError(t, os.WriteFile(path, []byte("edit"), 0o600))
	report.changes = append(report.changes, p.End("command").Changes...)
	review := report.Finish()
	require.Len(t, review.Changes, 1)
	require.Equal(t, "original", restoredBytes(t, review.Changes[0].ImportedState()))
	require.Equal(t, "edit", restoredBytes(t, review.Changes[0].After))
	require.Equal(t, 12, report.store.restoreRemaining)
}
