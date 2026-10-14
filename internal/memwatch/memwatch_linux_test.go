//go:build linux

package memwatch

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClaimNotifyOncePerWindow(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	start := time.Unix(1_000_000, 0)
	require.True(t, claimNotify(dir, start, NotifyEvery))
	require.False(t, claimNotify(dir, start.Add(time.Minute), NotifyEvery), "another window a minute later")
	require.True(t, claimNotify(dir, start.Add(NotifyEvery), NotifyEvery))
}

func TestReadsThisComputer(t *testing.T) {
	t.Parallel()
	u, err := readUsage()
	require.NoError(t, err)
	require.NotZero(t, u.MemTotal)
	require.NotEmpty(t, topPrograms(3))
}
