package filechange

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func beginCapture(p *ProcessReview, id string) *invocation {
	p.Begin(id, "writer "+id)
	return p.executed(nil, []string{"writer", id})
}

func TestReviewOverlappingAndLateActionsKeepReservations(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(map[bool]string{false: "overlapping", true: "late-report"}[late], func(t *testing.T) {
			root := visibleRestoreRoot(t)
			p := newProcessReview(root, nil)
			p.store.restoreRemaining = 12
			first := beginCapture(p, "first")
			firstPath := filepath.Join(root, "first")
			put(t, root, "first", "1234")
			p.before(first, firstPath)
			var pending *invocation
			if late {
				pending = p.executed(nil, []string{"writer", "pending"})
			} else {
				pending = beginCapture(p, "pending")
			}
			pendingPath := filepath.Join(root, "pending")
			put(t, root, "pending", "12345678")
			p.before(pending, pendingPath)
			require.Zero(t, p.store.restoreRemaining)
			require.NoError(t, os.Remove(firstPath))
			require.NotNil(t, p.End("first"))
			require.Equal(t, 4, p.store.restoreRemaining, "pending baselines must remain charged")
			third := beginCapture(p, "third")
			thirdPath := filepath.Join(root, "third")
			p.before(third, thirdPath)
			put(t, root, "third", "abcdefgh")
			review := p.End("third")
			require.Equal(t, RestoreBudgetReason, review.Changes[0].After.RestoreOmitted)
			require.Equal(t, 4, p.store.restoreRemaining)
			if late {
				p.Begin("pending", "writer pending")
			}
			require.NoError(t, os.Remove(pendingPath))
			review = p.End("pending")
			require.NotEmpty(t, review.Changes[0].Before.RestoreData)
			require.Equal(t, 12, p.store.restoreRemaining)
			// A descendant reporting after its action ended must not reserve again.
			child := p.executed(pending, []string{"writer", "late-child"})
			p.before(child, thirdPath)
			require.Nil(t, child.tracker)
			require.Equal(t, 12, p.store.restoreRemaining)
		})
	}
}

func TestSharedBaselineReleasedOnlyAfterLastOwner(t *testing.T) {
	root := visibleRestoreRoot(t)
	p := newProcessReview(root, nil)
	p.store.restoreRemaining = 12
	path := filepath.Join(root, "same")
	put(t, root, "same", "12345678")
	a, b := beginCapture(p, "a"), beginCapture(p, "b")
	p.before(a, path)
	p.before(b, path)
	require.Equal(t, 4, p.store.restoreRemaining)
	require.Nil(t, p.End("a")) // unchanged, but b still owns its before image
	require.Equal(t, 4, p.store.restoreRemaining)
	require.NoError(t, os.Remove(path))
	review := p.End("b")
	require.NotEmpty(t, review.Changes[0].Before.RestoreData)
	require.Equal(t, 12, p.store.restoreRemaining)
}

func TestNativeFinalizationReusesCapturedBytesAndIsStable(t *testing.T) {
	root := visibleRestoreRoot(t)
	ctx, report := WithCommandReview(t.Context(), root)
	report.store.restoreRemaining = 12
	path := filepath.Join(root, "file")
	BeforeOpen(ctx, path, os.O_CREATE|os.O_WRONLY)
	put(t, root, "file", "12345678")
	review := report.Finish()
	require.Len(t, review.Changes, 1)
	require.Empty(t, review.Changes[0].After.RestoreOmitted)
	data, err := base64.StdEncoding.DecodeString(review.Changes[0].After.RestoreData)
	require.NoError(t, err)
	require.Equal(t, "12345678", string(data))
	require.Equal(t, 12, report.store.restoreRemaining)
	put(t, root, "file", "unrelated later write")
	require.Same(t, review, report.Finish())
	require.Equal(t, 12, report.store.restoreRemaining)
}

func TestNativeSubprocessCapturesStayChargedUntilFinish(t *testing.T) {
	root := visibleRestoreRoot(t)
	_, report := WithCommandReview(t.Context(), root)
	report.store.restoreRemaining = 12
	for _, id := range []string{"a", "b"} {
		p := newProcessReview(root, nil)
		p.store, p.report = report.store, report
		record := beginCapture(p, id)
		path := filepath.Join(root, id)
		p.before(record, path)
		put(t, root, id, "12345678")
		review := p.End(id)
		require.NotNil(t, review)
		report.changes = append(report.changes, review.Changes...)
	}
	require.Equal(t, 4, report.store.restoreRemaining)
	review := report.Finish()
	require.Len(t, review.Changes, 2)
	require.NotEmpty(t, review.Changes[0].After.RestoreData)
	require.Equal(t, RestoreBudgetReason, review.Changes[1].After.RestoreOmitted)
	require.Equal(t, 12, report.store.restoreRemaining)
}

func TestImportedBaselineSurvivesOtherActionCompletion(t *testing.T) {
	root := visibleRestoreRoot(t)
	p := newProcessReview(root, nil)
	p.store.restoreRemaining = 20
	p.Begin("copy", "cp source destination")
	cp := p.executed(nil, []string{"cp", "source", "destination"})
	path := filepath.Join(root, "destination")
	p.before(cp, path)
	put(t, root, "destination", "12345678")
	copied := p.End("copy")
	require.Empty(t, copied.Changes[0].After.RestoreData)
	require.Equal(t, 20, p.store.restoreRemaining)
	edit := beginCapture(p, "edit")
	p.before(edit, path)
	other := beginCapture(p, "other")
	p.before(other, filepath.Join(root, "other"))
	put(t, root, "other", "abcd")
	require.NotNil(t, p.End("other"))
	require.Equal(t, 12, p.store.restoreRemaining)
	put(t, root, "destination", "edited!!")
	review := p.End("edit")
	require.NotEmpty(t, review.Changes[0].Before.RestoreData)
	require.NotEmpty(t, review.Changes[0].After.RestoreData)
	require.Equal(t, 20, p.store.restoreRemaining)
}

// Shell background children may outlive runner.Run. A closed native report
// must not reopen a fresh pool while their old tracker baselines still exist.
func TestNativeLateChildCannotAllocateAfterFinish(t *testing.T) {
	root := visibleRestoreRoot(t)
	_, report := WithCommandReview(t.Context(), root)
	report.store.restoreRemaining = 12
	p := newProcessReview(root, nil)
	p.store, p.report = report.store, report
	child := beginCapture(p, "detached")
	path := filepath.Join(root, "file")
	put(t, root, "file", "12345678")
	p.before(child, path)
	require.Equal(t, 4, report.store.restoreRemaining)
	require.Nil(t, report.Finish())
	require.True(t, report.store.closed)
	put(t, root, "file", "abcdefgh")
	review := p.End("detached")
	require.NotNil(t, review)
	require.Empty(t, review.Changes[0].After.RestoreData)
	require.Equal(t, "File review already finished", review.Changes[0].After.RestoreOmitted)
	require.Empty(t, report.store.captures)
	require.Nil(t, report.Finish())
}
