package db_test

import (
	"testing"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

// Fork and clone use the same copy helpers as a branch, so they bring the
// same extras (engine-design.md 5.11).
func TestCopyTreeNowCopiesExtras(t *testing.T) {
	s := newBranchStore(t)
	c := s.toolChat(t, "src")
	sibling := s.seedExtras(t, "src", c)
	tree := s.msgs.(message.TreeService)

	cloneID, _, err := tree.CopyTree(t.Context(), "src", c.u2.ID, false)
	require.NoError(t, err)
	sameMessages(t, []message.Message{c.u1, c.a1, c.t1, c.t2, c.a2, c.u2}, s.path(t, cloneID))
	s.checkExtras(t, cloneID, sibling)

	forkID, prompt, err := tree.CopyTree(t.Context(), "src", c.u2.ID, true)
	require.NoError(t, err)
	require.Equal(t, "Thanks", prompt)
	sameMessages(t, []message.Message{c.u1, c.a1, c.t1, c.t2, c.a2}, s.path(t, forkID))
	s.checkExtras(t, forkID, sibling)
}
