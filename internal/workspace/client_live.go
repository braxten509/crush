package workspace

import (
	"context"
	"sync"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/proto"
)

// LiveWork in client mode: the engine's routes (engine-design.md 5.3, 5.4
// and 5.9).

func (w *ClientWorkspace) Helpers(ctx context.Context, sessionID string) ([]agent.Task, error) {
	helpers, err := w.client.ListHelpers(ctx, w.workspaceID(), sessionID)
	if err != nil {
		return nil, err
	}
	tasks := make([]agent.Task, 0, len(helpers))
	for _, h := range helpers {
		tasks = append(tasks, taskFromHelper(h))
	}
	return tasks, nil
}

func (w *ClientWorkspace) StopHelper(ctx context.Context, sessionID, id string) error {
	return w.client.StopHelper(ctx, w.workspaceID(), sessionID, id)
}

func (w *ClientWorkspace) Jobs(ctx context.Context, sessionID string) ([]agent.Process, error) {
	jobs, err := w.client.ListJobs(ctx, w.workspaceID(), sessionID)
	if err != nil {
		return nil, err
	}
	procs := make([]agent.Process, 0, len(jobs))
	for _, j := range jobs {
		procs = append(procs, processFromJob(j))
	}
	return procs, nil
}

func (w *ClientWorkspace) KillJob(ctx context.Context, sessionID string, p agent.Process) error {
	ref := proto.JobRef{JobID: p.JobID}
	if ref.JobID == "" {
		ref.PID = p.PID
	}
	return w.client.KillJob(ctx, w.workspaceID(), sessionID, ref)
}

func (w *ClientWorkspace) CLILimits(ctx context.Context) ([]proto.CLILimits, error) {
	snap, err := w.client.Limits(ctx)
	return snap.CLIs, err
}

// shellOutputs routes the engine's shell_output events to the `!` commands
// of this app that wait for them, by request ID.
type shellOutputs struct {
	mu      sync.Mutex
	waiting map[string]*shellOutputWaiter
}

// shellOutputWaiter feeds one command's onProgress, in seq order. Once the
// command returned, it is closed and calls nothing again.
type shellOutputWaiter struct {
	mu         sync.Mutex
	onProgress func(string)
	next       int
	closed     bool
}

// wait registers requestID's onProgress; the returned func ends it.
func (s *shellOutputs) wait(requestID string, onProgress func(string)) (done func()) {
	waiter := &shellOutputWaiter{onProgress: onProgress}
	s.mu.Lock()
	if s.waiting == nil {
		s.waiting = make(map[string]*shellOutputWaiter)
	}
	s.waiting[requestID] = waiter
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.waiting, requestID)
		s.mu.Unlock()
		// After this, onProgress is never called: the caller may close
		// what it feeds.
		waiter.mu.Lock()
		waiter.closed = true
		waiter.mu.Unlock()
	}
}

// deliver hands one event's output to its command, if that still waits.
// An event older than one already delivered is dropped.
func (s *shellOutputs) deliver(out proto.ShellOutput) {
	s.mu.Lock()
	waiter := s.waiting[out.RequestID]
	s.mu.Unlock()
	if waiter == nil {
		return
	}
	waiter.mu.Lock()
	defer waiter.mu.Unlock()
	if waiter.closed || out.Seq < waiter.next {
		return
	}
	waiter.next = out.Seq + 1
	waiter.onProgress(out.Chunk)
}
