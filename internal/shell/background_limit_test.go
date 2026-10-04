package shell

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFinishedJobsDoNotBlockNewOnes(t *testing.T) {
	m := newBackgroundShellManager()
	for range MaxBackgroundJobs + 5 {
		job, err := m.Start(t.Context(), t.TempDir(), nil, "true", "quick")
		require.NoError(t, err, "finished jobs must not count toward the running limit")
		require.Eventually(t, job.IsDone, 10*time.Second, 10*time.Millisecond)
	}
	require.Len(t, m.List(), MaxBackgroundJobs+5, "finished jobs keep their output")
}

func TestOldestFinishedJobsAreDroppedPastTheCap(t *testing.T) {
	m := newBackgroundShellManager()
	start := time.Now().Unix() - 1000
	for i := range MaxRetainedCompletedJobs + 3 {
		job := &BackgroundShell{ID: fmt.Sprint(i), done: make(chan struct{})}
		close(job.done)
		job.completedAt.Store(start + int64(i))
		m.shells.Set(job.ID, job)
	}
	require.Equal(t, 3, m.Cleanup())
	require.Len(t, m.List(), MaxRetainedCompletedJobs)
	for job := range m.shells.Seq() {
		require.GreaterOrEqual(t, job.completedAt.Load(), start+3, "the oldest finished jobs go first")
	}
}
