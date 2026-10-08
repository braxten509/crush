package agent

import (
	"context"
	"sync"
	"time"

	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/google/uuid"
)

// sessionRun owns a non-interactive session until every producer of follow-up
// work has returned. A task keeps its reservation through result delivery, not
// just until its status changes, closing the finished-task -> queued-turn gap.
type sessionRun struct {
	ctx       context.Context
	cancel    context.CancelFunc
	sessionID string
	mu        sync.Mutex
	pending   int
	done      chan struct{}
	latest    notify.RunComplete
	hasLatest bool
	err       error
	followUps bool
	// Other accepted requests on the session still need their own correlated
	// terminal event when the session owner finishes.
	observers map[string]struct{}
	// turns counts the session's own turns in progress. With none running
	// and work still pending, the run is waiting on sub-agents or questions.
	turns int
	// status, when set, hears when that waiting starts and ends.
	status RunStatusFunc
	// reported is the waiting state last given to status; pendingReport
	// delays reporting a wait, since a finished turn often hands over to
	// the next within moments.
	reported      bool
	pendingReport *time.Timer
}

// RunStatusFunc hears when a headless run starts waiting on its sub-agents
// (true) and when it stops waiting (false).
type RunStatusFunc func(waiting bool)

type runStatusKey struct{}

// WithRunStatus asks a headless run to report when it is waiting on its
// sub-agents.
func WithRunStatus(ctx context.Context, fn RunStatusFunc) context.Context {
	return context.WithValue(ctx, runStatusKey{}, fn)
}

// waitReportDelay is how long a wait must last before it is reported.
var waitReportDelay = 500 * time.Millisecond

type sessionRunKey struct{}

// sessionFollowUpContext keeps the owner's cancellation while retaining the
// follow-up's provenance (hidden messages, CLI continuations, channels, etc.).
type sessionFollowUpContext struct {
	context.Context
	values context.Context
}

func (c sessionFollowUpContext) Value(key any) any {
	if value := c.values.Value(key); value != nil {
		return value
	}
	return c.Context.Value(key)
}

func newSessionRun(ctx context.Context, sessionID string) *sessionRun {
	// Scoped prompts must keep their own turn when queued behind another
	// run, rather than being folded into that run's next model step.
	if RunIDFromContext(ctx) == "" {
		ctx = WithRunID(ctx, uuid.NewString())
	}
	ctx, cancel := context.WithCancel(ctx)
	status, _ := ctx.Value(runStatusKey{}).(RunStatusFunc)
	r := &sessionRun{cancel: cancel, sessionID: sessionID, pending: 1, done: make(chan struct{}), status: status}
	r.ctx = context.WithValue(ctx, sessionRunKey{}, r)
	return r
}

func sessionRunFromContext(ctx context.Context, sessionID string) *sessionRun {
	r, _ := ctx.Value(sessionRunKey{}).(*sessionRun)
	if r != nil && r.sessionID == sessionID {
		return r
	}
	return nil
}

func (r *sessionRun) reserve() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pending == 0 {
		return false
	}
	r.pending++
	r.followUps = true
	return true
}

func (r *sessionRun) release(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err != nil && r.err == nil {
		r.err = err
	}
	r.pending--
	if r.pending == 0 {
		close(r.done)
	}
	r.updateStatus()
}

// turnStarted and turnEnded bracket each of the session's own turns.
func (r *sessionRun) turnStarted() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.turns++
	r.updateStatus()
}

func (r *sessionRun) turnEnded() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.turns--
	r.updateStatus()
}

// updateStatus reports a change in waiting; r.mu must be held. A wait is
// only reported once it outlasts [waitReportDelay], and its end only if it
// was reported.
func (r *sessionRun) updateStatus() {
	if r.status == nil {
		return
	}
	waiting := r.turns == 0 && r.pending > 0
	if !waiting {
		if r.pendingReport != nil {
			r.pendingReport.Stop()
			r.pendingReport = nil
		}
		if r.reported {
			r.reported = false
			r.status(false)
		}
		return
	}
	if r.reported || r.pendingReport != nil {
		return
	}
	var timer *time.Timer
	timer = time.AfterFunc(waitReportDelay, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.pendingReport != timer {
			return
		}
		r.pendingReport = nil
		if r.turns == 0 && r.pending > 0 {
			r.reported = true
			r.status(true)
		}
	})
	r.pendingReport = timer
}

func (r *sessionRun) record(complete notify.RunComplete) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.latest, r.hasLatest = complete, true
}

func (r *sessionRun) observe(runID string) bool {
	if runID == "" || runID == RunIDFromContext(r.ctx) {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.observers == nil {
		r.observers = make(map[string]struct{})
	}
	r.observers[runID] = struct{}{}
	return true
}

func (r *sessionRun) observerIDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var ids []string
	for id := range r.observers {
		ids = append(ids, id)
	}
	return ids
}

func (r *sessionRun) wait() (notify.RunComplete, bool, error) {
	select {
	case <-r.ctx.Done():
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.latest, r.hasLatest, r.ctx.Err()
	case <-r.done:
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.latest, r.hasLatest, r.err
	}
}

// beginSessionRun joins an existing run for a follow-up or creates its owner.
func (h *taskHub) beginSessionRun(ctx context.Context, sessionID string) (*sessionRun, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r := h.sessionRuns[sessionID]; r != nil && r.reserve() {
		return r, false
	}
	r := newSessionRun(ctx, sessionID)
	if h.sessionRuns == nil {
		h.sessionRuns = make(map[string]*sessionRun)
	}
	h.sessionRuns[sessionID] = r
	return r, true
}

func (h *taskHub) endSessionRun(r *sessionRun) {
	r.cancel()
	r.mu.Lock()
	if r.pendingReport != nil {
		r.pendingReport.Stop()
		r.pendingReport = nil
	}
	r.status = nil
	r.mu.Unlock()
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, run := range h.sessionRuns {
		if run == r {
			delete(h.sessionRuns, id)
		}
	}
}

// reserveFollowUp is held from before a task/form is acknowledged until its
// follow-up Run has returned, including time spent waiting for a result.
func (h *taskHub) reserveFollowUp(sessionID string) (context.Context, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r := h.sessionRuns[sessionID]; r != nil {
		if r.reserve() {
			return r.ctx, func() { r.release(nil) }
		}
		// A completed run may still be awaiting removal from the map. New
		// requests are independent of it and must not inherit its cancellation.
	}
	return context.Background(), func() {}
}
