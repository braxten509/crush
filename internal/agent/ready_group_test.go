package agent

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Turns wait for readiness while UpdateModels registers new setup work.
// errgroup.Group panicked here with "WaitGroup is reused before previous
// Wait has returned".
func TestReadyGroupGoDuringWaitDoesNotPanic(t *testing.T) {
	var g readyGroup
	stop := make(chan struct{})
	var waiters sync.WaitGroup
	for range 8 {
		waiters.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
					if err := g.Wait(); err != nil {
						t.Error(err)
						return
					}
				}
			}
		})
	}
	for range 2000 {
		g.Go(func() error { return nil })
	}
	close(stop)
	waiters.Wait()
	require.NoError(t, g.Wait())
}

func TestReadyGroupWaitsForTasksAddedByTasks(t *testing.T) {
	var g readyGroup
	var nestedDone atomic.Bool
	g.Go(func() error {
		g.Go(func() error {
			time.Sleep(50 * time.Millisecond)
			nestedDone.Store(true)
			return nil
		})
		return nil
	})
	require.NoError(t, g.Wait())
	require.True(t, nestedDone.Load())
}

func TestReadyGroupErrorIsSticky(t *testing.T) {
	var g readyGroup
	boom := errors.New("boom")
	g.Go(func() error { return boom })
	g.Go(func() error { return nil })
	require.ErrorIs(t, g.Wait(), boom)
	g.Go(func() error { return nil })
	require.ErrorIs(t, g.Wait(), boom)
}
