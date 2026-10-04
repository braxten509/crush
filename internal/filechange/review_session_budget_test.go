package filechange

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReviewSequentialActionsRetainRestoreSupport(t *testing.T) {
	root := visibleRestoreRoot(t)
	p := newProcessReview(root, nil)
	// Scale the shared limit down: each completed action needs only eight
	// bytes, so two separate actions should both fit a twelve-byte limit.
	p.store.restoreRemaining = 12
	for i := range 2 {
		id := fmt.Sprint(i)
		p.Begin(id, "writer "+id)
		r := p.executed(nil, []string{"writer", id})
		path := filepath.Join(root, "file-"+id)
		p.before(r, path)
		require.NoError(t, os.WriteFile(path, []byte("12345678"), 0600))
		review := p.End(id)
		require.NotNil(t, review)
		require.Len(t, review.Changes, 1)
		require.Empty(t, review.Changes[0].After.RestoreOmitted, "completed action %s exhausted later actions' budget", id)
		require.NotEmpty(t, review.Changes[0].After.RestoreData)
	}
}
