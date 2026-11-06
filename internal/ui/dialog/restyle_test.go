package dialog

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Every dialog and list item in this package that copies styles or caches
// renders must be restyled when the theme changes in place. The dialogs in
// the subpackages assert the same in their own tests.
var (
	_ StyleRefresher = (*Commands)(nil)
	_ StyleRefresher = (*Reasoning)(nil)
	_ StyleRefresher = (*Notifications)(nil)
	_ StyleRefresher = (*Arguments)(nil)
	_ StyleRefresher = (*FilePicker)(nil)
	_ StyleRefresher = (*CommandItem)(nil)
	_ StyleRefresher = (*ReasoningItem)(nil)
	_ StyleRefresher = (*NotificationItem)(nil)
)

// refreshCounter counts how often it was asked to pick up the styles.
type refreshCounter struct{ n int }

func (r *refreshCounter) RefreshStyles() { r.n++ }

// TestRefreshItems_ReachesEveryItem checks every item that restyles is
// asked, and items that do not are skipped.
func TestRefreshItems_ReachesEveryItem(t *testing.T) {
	t.Parallel()
	a, b := &refreshCounter{}, &refreshCounter{}
	RefreshItems([]any{a, "not a refresher", b})
	require.Equal(t, 1, a.n)
	require.Equal(t, 1, b.n)
}
