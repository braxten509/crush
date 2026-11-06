package shell

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/crush/internal/filechange"
)

const (
	// MaxBackgroundJobs is the default number of background jobs that may
	// run at once. It is used when Start is given no positive limit of its
	// own (the configured limit, see config.Options.GetMaxBackgroundJobs).
	MaxBackgroundJobs = 50
	// MaxFinishedJobs bounds how many finished jobs keep their output
	// readable. The oldest are dropped first.
	MaxFinishedJobs = 250
	// MaxRetainedCompletedJobs is the older name for MaxFinishedJobs.
	MaxRetainedCompletedJobs = MaxFinishedJobs
	// CompletedJobRetention is how long a finished job's output stays
	// readable.
	CompletedJobRetention = 8 * time.Hour
	// CompletedJobRetentionMinutes is CompletedJobRetention in minutes.
	CompletedJobRetentionMinutes = int(CompletedJobRetention / time.Minute)
)

// BackgroundShell represents a shell running in the background.
type BackgroundShell struct {
	ID          string
	Started     time.Time
	Command     string
	Description string
	Shell       *Shell
	WorkingDir  string
	ctx         context.Context
	cancel      context.CancelFunc
	stdout      *cappedBuffer
	stderr      *cappedBuffer
	done        chan struct{}
	exitErr     error
	review      *filechange.Review
	completedAt atomic.Int64 // Unix timestamp when job completed (0 if still running)
}

// BackgroundShellManager manages background shell instances.
type BackgroundShellManager struct {
	// mu guards shells. Start holds it across its limit check and the
	// listing of the new job, so jobs started at once can't pass the limit
	// together.
	mu     sync.Mutex
	shells map[string]*BackgroundShell
}

var (
	backgroundManager     *BackgroundShellManager
	backgroundManagerOnce sync.Once
	idCounter             atomic.Uint64
)

// newBackgroundShellManager creates a new BackgroundShellManager instance.
func newBackgroundShellManager() *BackgroundShellManager {
	return &BackgroundShellManager{shells: map[string]*BackgroundShell{}}
}

// GetBackgroundShellManager returns the singleton background shell manager.
func GetBackgroundShellManager() *BackgroundShellManager {
	backgroundManagerOnce.Do(func() {
		backgroundManager = newBackgroundShellManager()
	})
	return backgroundManager
}

// LeftoverWait is how long a background job's command may keep its output
// open after it has exited. A command can leave helpers behind (D-Bus starts
// portals and secret stores on demand) that inherit the output and keep it
// open for hours; without a limit the job would show as running until they
// quit. A variable so tests can shorten it.
var LeftoverWait = 3 * time.Second

type leftoverWaitKey struct{}

// leftoverWait is the LeftoverWait of the background job running under ctx;
// zero (wait for every holder of the output) for foreground commands.
func leftoverWait(ctx context.Context) time.Duration {
	wait, _ := ctx.Value(leftoverWaitKey{}).(time.Duration)
	return wait
}

// Start creates and starts a new background shell with the given command,
// unless maxJobs jobs are already running. A maxJobs of zero or less means
// the default, MaxBackgroundJobs. Only running jobs count toward the limit:
// finished jobs are kept for their output, but must never block new work.
// extraEnv is added to this job's environment only.
func (m *BackgroundShellManager) Start(ctx context.Context, workingDir string, blockFuncs []BlockFunc, command, description string, maxJobs int, extraEnv ...string) (*BackgroundShell, error) {
	if maxJobs <= 0 {
		maxJobs = MaxBackgroundJobs
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if running := m.prune(); running >= maxJobs {
		return nil, fmt.Errorf("maximum number of running background jobs (%d) reached. Please stop or wait for some jobs to complete", maxJobs)
	}

	id := fmt.Sprintf("%03X", idCounter.Add(1))

	shell := NewShell(&Options{
		WorkingDir: workingDir,
		BlockFuncs: blockFuncs,
		Env:        append(os.Environ(), extraEnv...),
	})

	shellCtx, cancel := context.WithCancel(context.WithValue(ctx, leftoverWaitKey{}, LeftoverWait))

	bgShell := &BackgroundShell{
		ID:          id,
		Started:     time.Now(),
		Command:     command,
		Description: description,
		WorkingDir:  workingDir,
		Shell:       shell,
		ctx:         shellCtx,
		cancel:      cancel,
		stdout:      newCappedBuffer(maxKeptOutput),
		stderr:      newCappedBuffer(maxKeptOutput),
		done:        make(chan struct{}),
	}

	m.shells[id] = bgShell

	go func() {
		defer close(bgShell.done)

		err := shell.ExecStream(shellCtx, command, bgShell.stdout, bgShell.stderr)

		if report := filechange.CommandReviewFromContext(shellCtx); report != nil {
			bgShell.review = report.Finish()
		}
		bgShell.exitErr = err
		bgShell.completedAt.Store(time.Now().Unix())
	}()

	return bgShell, nil
}

// prune drops finished jobs that are past retention or beyond
// MaxFinishedJobs, and reports how many jobs are still running. Callers
// must hold m.mu.
func (m *BackgroundShellManager) prune() (running int) {
	cutoff := time.Now().Add(-CompletedJobRetention).Unix()
	var finished []*BackgroundShell
	for id, shell := range m.shells {
		switch at := shell.completedAt.Load(); {
		case at == 0:
			running++
		case at < cutoff:
			delete(m.shells, id)
		default:
			finished = append(finished, shell)
		}
	}

	if over := len(finished) - MaxFinishedJobs; over > 0 {
		slices.SortFunc(finished, func(a, b *BackgroundShell) int {
			return cmp.Compare(a.completedAt.Load(), b.completedAt.Load())
		})
		for _, shell := range finished[:over] {
			delete(m.shells, shell.ID)
		}
	}
	return running
}

// Cleanup drops finished jobs that are past retention or beyond
// MaxFinishedJobs (oldest first) and reports how many it dropped. Start
// already does this before every new job; Cleanup is for callers that want
// it done on its own.
func (m *BackgroundShellManager) Cleanup() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	before := len(m.shells)
	m.prune()
	return before - len(m.shells)
}

// take removes and returns the shell with the given ID.
func (m *BackgroundShellManager) take(id string) (*BackgroundShell, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	shell, ok := m.shells[id]
	if !ok {
		return nil, fmt.Errorf("background shell not found: %s", id)
	}
	delete(m.shells, id)
	return shell, nil
}

// Get retrieves a background shell by ID.
func (m *BackgroundShellManager) Get(id string) (*BackgroundShell, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	shell, ok := m.shells[id]
	return shell, ok
}

// Remove stops tracking a background shell without terminating it.
func (m *BackgroundShellManager) Remove(id string) error {
	_, err := m.take(id)
	return err
}

// Kill terminates a background shell by ID.
func (m *BackgroundShellManager) Kill(id string) error {
	shell, err := m.take(id)
	if err != nil {
		return err
	}
	shell.cancel()
	<-shell.done
	return nil
}

// BackgroundShellInfo contains information about a background shell.
type BackgroundShellInfo struct {
	ID          string
	Command     string
	Description string
}

// List returns all background shell IDs.
func (m *BackgroundShellManager) List() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Collect(maps.Keys(m.shells))
}

// KillAll terminates all background shells. The provided context bounds how
// long the function waits for each shell to exit.
func (m *BackgroundShellManager) KillAll(ctx context.Context) {
	m.mu.Lock()
	shells := m.shells
	m.shells = map[string]*BackgroundShell{}
	m.mu.Unlock()

	var wg sync.WaitGroup
	for _, shell := range shells {
		wg.Go(func() {
			shell.cancel()
			select {
			case <-shell.done:
			case <-ctx.Done():
			}
		})
	}
	wg.Wait()
}

// GetOutput returns the current output of a background shell.
func (bs *BackgroundShell) GetOutput() (stdout string, stderr string, done bool, err error) {
	select {
	case <-bs.done:
		return bs.stdout.String(), bs.stderr.String(), true, bs.exitErr
	default:
		return bs.stdout.String(), bs.stderr.String(), false, nil
	}
}

// IsDone checks if the background shell has finished execution.
func (bs *BackgroundShell) IsDone() bool {
	select {
	case <-bs.done:
		return true
	default:
		return false
	}
}

// Done returns a channel that is closed when the background shell exits.
func (bs *BackgroundShell) Done() <-chan struct{} {
	return bs.done
}

// Wait blocks until the background shell completes.
func (bs *BackgroundShell) Wait() {
	<-bs.done
}

func (bs *BackgroundShell) WaitContext(ctx context.Context) bool {
	select {
	case <-bs.done:
		return true
	case <-ctx.Done():
		return false
	}
}

// Review returns the immutable final report once the job has completed.
func (bs *BackgroundShell) Review() *filechange.Review {
	select {
	case <-bs.done:
		return bs.review
	default:
		return nil
	}
}
