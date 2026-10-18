package lock

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTryFile_AcquiresWhenFree(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.lock")

	release, err := TryFile(path)
	require.NoError(t, err)
	require.NotNil(t, release)
	release()
}

func TestTryFile_ReturnsErrContendedWhenHeld(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.lock")

	release, err := TryFile(path)
	require.NoError(t, err)
	t.Cleanup(release)

	_, err = TryFile(path)
	require.ErrorIs(t, err, ErrContended)
}

func TestTryFile_ReacquireAfterRelease(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.lock")

	release, err := TryFile(path)
	require.NoError(t, err)
	release()

	release2, err := TryFile(path)
	require.NoError(t, err)
	t.Cleanup(release2)
}

func TestFile_AcquiresWhenFree(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.lock")

	release, err := File(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(release)
}

func TestFile_BlocksThenSucceeds(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.lock")

	release, err := TryFile(path)
	require.NoError(t, err)

	// Release the lock after a short delay so the blocking acquirer
	// can complete within the test timeout.
	go func() {
		time.Sleep(150 * time.Millisecond)
		release()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	release2, err := File(ctx, path)
	require.NoError(t, err, "should acquire after first releases")
	release2()
}

func TestFile_RespectsContextDeadline(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.lock")

	release, err := TryFile(path)
	require.NoError(t, err)
	t.Cleanup(release)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = File(ctx, path)
	elapsed := time.Since(start)
	require.Error(t, err)
	require.True(t, errors.Is(err, context.DeadlineExceeded), "expected deadline exceeded, got %v", err)
	require.Less(t, elapsed, 1*time.Second, "should return promptly after deadline")
}

func TestFile_RespectsContextCancellation(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.lock")

	release, err := TryFile(path)
	require.NoError(t, err)
	t.Cleanup(release)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := File(ctx, path)
		done <- err
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("File did not return after context cancellation")
	}
}

// TestFile_ConcurrentAcquirers verifies that multiple blocking acquirers
// queue up correctly: each gets the lock in turn, exactly one at a time.
func TestFile_ConcurrentAcquirers(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.lock")

	const n = 5
	var (
		mu       sync.Mutex
		inside   int
		maxSeen  int
		finished int
	)
	var wg sync.WaitGroup
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			release, err := File(ctx, path)
			if err != nil {
				t.Errorf("acquire failed: %v", err)
				return
			}
			mu.Lock()
			inside++
			if inside > maxSeen {
				maxSeen = inside
			}
			mu.Unlock()

			time.Sleep(20 * time.Millisecond)

			mu.Lock()
			inside--
			finished++
			mu.Unlock()
			release()
		}()
	}
	wg.Wait()

	require.Equal(t, n, finished)
	require.Equal(t, 1, maxSeen, "lock must be mutually exclusive")
}

// tryWithin runs try in a goroutine and fails the test when it has not
// returned within a second, so a lock call that waits instead of refusing
// fails here instead of hanging the test.
func tryWithin(t *testing.T, try func() (func(), error)) (func(), error) {
	t.Helper()
	type result struct {
		release func()
		err     error
	}
	done := make(chan result, 1)
	go func() {
		release, err := try()
		done <- result{release, err}
	}()
	select {
	case r := <-done:
		return r.release, r.err
	case <-time.After(time.Second):
		t.Fatal("the lock call waited instead of answering at once")
		return nil, nil
	}
}

// TestSharedLocksCoexist proves two shared holders, each on its own file
// descriptor, hold the lock together (engine-design.md 5.10).
func TestSharedLocksCoexist(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the shared lock is tested on Linux and Mac")
	}
	path := filepath.Join(t.TempDir(), "test.lock")

	first, err := tryWithin(t, func() (func(), error) { return TryFileShared(path) })
	require.NoError(t, err)
	t.Cleanup(first)

	second, err := tryWithin(t, func() (func(), error) { return TryFileShared(path) })
	require.NoError(t, err, "a second shared holder must get the lock too")
	t.Cleanup(second)
}

// TestExclusiveBlocksShared proves a shared lock is refused at once while
// an exclusive holder has the file, and granted once it lets go.
func TestExclusiveBlocksShared(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.lock")

	release, err := TryFile(path)
	require.NoError(t, err)

	_, err = tryWithin(t, func() (func(), error) { return TryFileShared(path) })
	require.ErrorIs(t, err, ErrContended)

	release()
	shared, err := tryWithin(t, func() (func(), error) { return TryFileShared(path) })
	require.NoError(t, err)
	shared()
}

// TestSharedBlocksExclusive proves an exclusive lock is refused at once
// while a shared holder has the file, and granted once it lets go.
func TestSharedBlocksExclusive(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.lock")

	shared, err := TryFileShared(path)
	require.NoError(t, err)

	_, err = tryWithin(t, func() (func(), error) { return TryFile(path) })
	require.ErrorIs(t, err, ErrContended)

	shared()
	release, err := tryWithin(t, func() (func(), error) { return TryFile(path) })
	require.NoError(t, err)
	release()
}
