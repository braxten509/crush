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

func TestReviewOverlappingAndLateReportsKeepTheirAllocation(t *testing.T) {
	for _, late := range []bool{false, true} {
		name := "overlapping"
		if late {
			name = "late-report"
		}
		t.Run(name, func(t *testing.T) {
			root := visibleRestoreRoot(t)
			p := newProcessReview(root, nil)
			p.store.restoreRemaining = 12
			path := filepath.Join(root, "pending")
			require.NoError(t, os.WriteFile(path, []byte("original"), 0600))
			if !late {
				p.Begin("pending", "pending-writer")
			}
			pending := p.executed(nil, []string{"pending-writer"})
			p.before(pending, path)
			require.Equal(t, 4, p.store.restoreRemaining)
			// A different completed action cannot reset the pending baseline's charge.
			p.Begin("first", "first-writer")
			first := p.executed(nil, []string{"first-writer"})
			small := filepath.Join(root, "small")
			p.before(first, small)
			require.NoError(t, os.WriteFile(small, []byte("1234"), 0600))
			require.Equal(t, "1234", restoredBytes(t, p.End("first").Changes[0].After))
			require.Equal(t, 4, p.store.restoreRemaining)
			p.Begin("overflow", "overflow-writer")
			overflow := p.executed(nil, []string{"overflow-writer"})
			large := filepath.Join(root, "large")
			p.before(overflow, large)
			require.NoError(t, os.WriteFile(large, []byte("12345678"), 0600))
			review := p.End("overflow")
			require.Equal(t, RestoreBudgetReason, review.Changes[0].After.RestoreOmitted)
			require.Equal(t, 4, p.store.restoreRemaining)
			if late {
				p.Begin("pending", "pending-writer")
			}
			require.NoError(t, os.WriteFile(path, []byte("edit"), 0600))
			review = p.End("pending")
			require.Equal(t, "original", restoredBytes(t, review.Changes[0].Before))
			require.Equal(t, "edit", restoredBytes(t, review.Changes[0].After))
			require.Equal(t, 12, p.store.restoreRemaining)
			require.Empty(t, p.store.captures)
			require.Empty(t, p.store.text)
		})
	}
}

func TestReviewSharedBaselineReleasedAfterLastOwner(t *testing.T) {
	root := visibleRestoreRoot(t)
	p := newProcessReview(root, nil)
	p.store.restoreRemaining = 12
	path := filepath.Join(root, "shared")
	require.NoError(t, os.WriteFile(path, []byte("original"), 0600))
	for _, id := range []string{"one", "two"} {
		p.Begin(id, id)
		record := p.executed(nil, []string{id})
		p.before(record, path)
	}
	require.Equal(t, 4, p.store.restoreRemaining, "unchanged baseline is captured once")
	require.Nil(t, p.End("one"))
	require.Equal(t, 4, p.store.restoreRemaining, "second owner still needs the baseline")
	require.NoError(t, os.WriteFile(path, []byte("edit"), 0600))
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
			require.NoError(t, os.WriteFile(path, []byte("original"), 0600))
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
			require.NoError(t, os.WriteFile(path, []byte("edit"), 0600))
			if subprocess {
				report.changes = append(report.changes, process.End("command").Changes...)
				require.Zero(t, report.store.restoreRemaining)
			}
			review := report.Finish()
			require.Len(t, review.Changes, 1)
			require.Equal(t, "original", restoredBytes(t, review.Changes[0].Before))
			require.Equal(t, "edit", restoredBytes(t, review.Changes[0].After))
			require.Equal(t, 12, report.store.restoreRemaining)
			require.Empty(t, report.store.captures)
			require.NoError(t, os.WriteFile(path, []byte("later"), 0600))
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
	require.NoError(t, os.WriteFile(path, []byte("original"), 0600))
	p.before(wrapper, path)
	require.NoError(t, os.WriteFile(path, []byte("edit"), 0600))
	report.changes = append(report.changes, p.End("command").Changes...)
	review := report.Finish()
	require.Len(t, review.Changes, 1)
	require.Equal(t, "original", restoredBytes(t, review.Changes[0].ImportedState()))
	require.Equal(t, "edit", restoredBytes(t, review.Changes[0].After))
	require.Equal(t, 12, report.store.restoreRemaining)
}
