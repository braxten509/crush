package agent

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/message"
)

// sessionScheduler queues and dispatches the runs of an agent's sessions.
// Each session runs one turn at a time: a prompt that arrives while the
// session is busy waits in the queue and joins the active turn at its next
// tool boundary, or runs as the next batch once it ends. The scheduler also
// tracks prompts accepted by the fire-and-forget dispatch path before they
// reach Run, so a cancel never misses one, and hands queued prompts over to
// the replacement turn of an Interrupt.
type sessionScheduler struct {
	host schedulerHost

	messageQueue   *csync.Map[string, []SessionAgentCall]
	activeRequests *csync.Map[string, *activeCancel]
	// steering holds the running agent CLI turn per session, whose steered
	// prompts still count as queued until the CLI takes them in.
	steering *csync.Map[string, *cliSteps]
	// interrupting holds prompts an Interrupt will run once the canceled run
	// ends; they count as queued and keep the session busy meanwhile.
	interrupting *csync.Map[string, []SessionAgentCall]
	// handoffs owns a queue snapshot, including acceptance leases not dispatched yet.
	handoffs *csync.Map[string, *queueHandoff]

	// dispatchMu holds a per-session mutex that serializes the
	// accepted -> (cancel-on-entry | queued | active) transition in
	// Run against a concurrent Cancel. The lock is held only during
	// the brief handoff (no DB or LLM I/O under the lock).
	dispatchMu *csync.Map[string, *sync.Mutex]
	// acceptedRuns counts dispatched-but-not-yet-active runs per
	// session. A counter > 0 means a dispatched prompt is in flight
	// and has not yet completed the dispatch handoff in Run. Only
	// BeginAccepted increments it; only AcceptedRun.Close decrements
	// it.
	acceptedRuns *csync.Map[string, int]
	// cancelMark records, per session, a high-water accept sequence: an
	// accepted handle is canceled by it iff the handle's sequence is at
	// or below the mark. Cancel raises the mark to the latest sequence
	// assigned at cancel time, so a single Cancel covers every prompt
	// accepted-but-not-yet-active then, while a prompt accepted later
	// (higher sequence) is never poisoned. Absent or 0 means no pending
	// cancel. It is only raised by Cancel when acceptedRuns > 0, so an
	// idle Escape never records a mark.
	cancelMark *csync.Map[string, uint64]
	// dispatchMuCreate guards lazy creation of per-session entries in
	// dispatchMu so two goroutines can't race to lock different mutex
	// instances for the same session.
	dispatchMuCreate sync.Mutex
	// acceptedMu serializes increments/decrements of acceptedRuns and
	// the assignment of accept sequence numbers from acceptSeqGen. It
	// is separate from dispatchMu so AcceptedRun.Close (which may run
	// while Run holds dispatchMu for the same session) does not
	// deadlock by re-entering the dispatch lock.
	acceptedMu sync.Mutex
	// acceptSeqGen is the monotonic source of accept sequence numbers.
	// Acceptance and untracked enqueue increment it under acceptedMu,
	// so sequences strictly increase in acceptance/enqueue order
	// across the agent. Cancel uses its current value as the per-session
	// high-water mark.
	acceptSeqGen uint64
	// acceptedLeases is guarded by acceptedMu; Close removes the exact lease.
	acceptedLeases map[string]map[uint64]*AcceptedRun
}

// schedulerHost is what the scheduler needs from the agent it schedules
// runs for.
type schedulerHost interface {
	// Run runs a call the scheduler dispatched.
	Run(ctx context.Context, call SessionAgentCall) (*fantasy.AgentResult, error)
	// publishRunComplete publishes the terminal event of a call.
	publishRunComplete(ctx context.Context, call SessionAgentCall, complete notify.RunComplete)
	// queuedChannelReply returns how replies to a channel are routed, or
	// nil when that is unknown.
	queuedChannelReply(channel string) *config.MCPChannelReply
}

func newSessionScheduler(host schedulerHost) sessionScheduler {
	return sessionScheduler{
		host:           host,
		messageQueue:   csync.NewMap[string, []SessionAgentCall](),
		activeRequests: csync.NewMap[string, *activeCancel](),
		steering:       csync.NewMap[string, *cliSteps](),
		interrupting:   csync.NewMap[string, []SessionAgentCall](),
		handoffs:       csync.NewMap[string, *queueHandoff](),
		acceptedLeases: make(map[string]map[uint64]*AcceptedRun),
		dispatchMu:     csync.NewMap[string, *sync.Mutex](),
		acceptedRuns:   csync.NewMap[string, int](),
		cancelMark:     csync.NewMap[string, uint64](),
	}
}

// queuedChannelReply uses the same configured or discovered routing as
// sendChannelReply.
func (a *sessionAgent) queuedChannelReply(channel string) *config.MCPChannelReply {
	if a.cfg == nil {
		return nil
	}
	mcpCfg, ok := a.cfg.Config().MCP[channel]
	if !ok {
		return nil
	}
	if mcpCfg.ChannelReply != nil {
		return mcpCfg.ChannelReply
	}
	return discoverChannelReply(channel)
}

// BeginAccepted increments the accept counter for sessionID and returns
// a handle whose Close is the only way to decrement it. It is the only
// entry point that mutates acceptedRuns.
func (s *sessionScheduler) BeginAccepted(sessionID string) *AcceptedRun {
	mu := s.sessionMu(sessionID)
	mu.Lock()
	defer mu.Unlock()
	return s.beginAcceptedLocked(sessionID)
}

// beginAcceptedLocked requires dispatchMu, including for internal batch reservations.
func (s *sessionScheduler) beginAcceptedLocked(sessionID string) *AcceptedRun {
	s.acceptedMu.Lock()
	defer s.acceptedMu.Unlock()
	count, _ := s.acceptedRuns.Get(sessionID)
	s.acceptedRuns.Set(sessionID, count+1)
	s.acceptSeqGen++
	followUp := s.IsSessionBusy(sessionID)
	for _, earlier := range s.acceptedLeases[sessionID] {
		if !earlier.done.Load() && !earlier.canceled.Load() && !s.canceledBySeq(sessionID, earlier.seq) {
			followUp = true
			break
		}
	}
	r := &AcceptedRun{sched: s, sessionID: sessionID, seq: s.acceptSeqGen, followUp: followUp}
	if s.acceptedLeases[sessionID] == nil {
		s.acceptedLeases[sessionID] = make(map[uint64]*AcceptedRun)
	}
	s.acceptedLeases[sessionID][r.seq] = r
	return r
}

// endAccepted decrements the accept counter for sessionID. It is only
// called via AcceptedRun.Close. It uses a dedicated lock (not the
// per-session dispatch mutex) so it can run while Run holds dispatchMu
// for the same session without deadlocking.
//
// When the count reaches zero the session's cancel mark is dropped: no
// accepted handle remains for it to cover, and any handle accepted later
// gets a strictly higher sequence that the mark would not match anyway.
// Handles canceled on entry never reach RunComplete, so this is the only
// place that clears the mark for an all-canceled batch. Sibling handles
// covered by the same mark are serialized on the per-session dispatch
// mutex and read the mark before they Close, so this never clears it out
// from under a covered handle still waiting to enter Run.
func (s *sessionScheduler) endAccepted(r *AcceptedRun) {
	s.acceptedMu.Lock()
	defer s.acceptedMu.Unlock()
	sessionID := r.sessionID
	delete(s.acceptedLeases[sessionID], r.seq)
	if len(s.acceptedLeases[sessionID]) == 0 {
		delete(s.acceptedLeases, sessionID)
	}
	if r.handoff != nil {
		r.handoff.pending--
		r.handoff.signal()
	}
	count, ok := s.acceptedRuns.Get(sessionID)
	if !ok || count <= 1 {
		s.acceptedRuns.Del(sessionID)
		s.cancelMark.Del(sessionID)
		return
	}
	s.acceptedRuns.Set(sessionID, count-1)
}

// sessionMu returns the per-session dispatch mutex, creating it on first
// use. Creation is guarded so concurrent callers always observe the same
// mutex instance for a given session.
func (s *sessionScheduler) sessionMu(sessionID string) *sync.Mutex {
	if mu, ok := s.dispatchMu.Get(sessionID); ok {
		return mu
	}
	s.dispatchMuCreate.Lock()
	defer s.dispatchMuCreate.Unlock()
	if mu, ok := s.dispatchMu.Get(sessionID); ok {
		return mu
	}
	mu := &sync.Mutex{}
	s.dispatchMu.Set(sessionID, mu)
	return mu
}

// enqueueCall appends call to the session's message queue. The
// OnComplete hook is stripped: the caller that supplied it (typically
// coordinator.Run) has its own retry/coalesce scope that ends when it
// returns, so by the time the queue drains nobody is left to consume the
// buffered terminal event. The recursive Run falls back to the default
// broker publish, which is what existing subscribers expect for queued
// turns.
func (s *sessionScheduler) enqueueCall(call SessionAgentCall) {
	if len(call.batch) > 0 && !call.NotRecallable {
		for _, original := range call.batch {
			original.Accepted = call.Accepted
			s.enqueueCall(original)
		}
		return
	}
	existing, ok := s.messageQueue.Get(call.SessionID)
	if !ok {
		existing = []SessionAgentCall{}
	}
	queued := call
	if call.Accepted != nil {
		// Preserve the accept sequence after the handle is stripped so
		// the queue-drain paths can tell a follow-up queued before a
		// cancel (covered by the mark) from one queued after it.
		queued.acceptSeq = call.Accepted.seq
	}
	if call.sessionRun == nil {
		queued.OnComplete = nil
	}
	if queued.queueOrder == 0 {
		s.acceptedMu.Lock()
		if call.Accepted != nil {
			queued.queueOrder = call.Accepted.seq
		} else {
			s.acceptSeqGen++
			queued.queueOrder = s.acceptSeqGen
		}
		s.acceptedMu.Unlock()
	}
	queued.Accepted = nil
	if call.Accepted != nil && call.Accepted.handoff != nil {
		pending, _ := s.interrupting.Get(call.SessionID)
		s.interrupting.Set(call.SessionID, appendQueuedInOrder(pending, queued))
		return
	}
	existing = appendQueuedInOrder(existing, queued)
	s.messageQueue.Set(call.SessionID, existing)
	if s, _ := s.steering.Get(call.SessionID); s != nil {
		select {
		case s.steerReady <- struct{}{}:
		default:
		}
	}
}

// requeueFront puts calls back at the front of the session's queue.
func (s *sessionScheduler) requeueFront(sessionID string, calls []SessionAgentCall) {
	mu := s.sessionMu(sessionID)
	mu.Lock()
	defer mu.Unlock()
	s.requeueFrontLocked(sessionID, calls)
}

// requeueFrontLocked requires the session's dispatch mutex.
func (s *sessionScheduler) requeueFrontLocked(sessionID string, calls []SessionAgentCall) {
	queued, _ := s.messageQueue.Get(sessionID)
	s.messageQueue.Set(sessionID, append(slices.Clone(calls), queued...))
}

// compatibleQueuedCalls keeps channel tool scope and reply destinations
// separate. CLI continuations and summarization continuations must also keep
// their own dispatch semantics. Compatible adjacent submissions form one batch.
func (s *sessionScheduler) compatibleQueuedCalls(first, next SessionAgentCall) bool {
	if first.CLIContinue || next.CLIContinue || first.NotRecallable || next.NotRecallable {
		return false
	}
	if first.Channel != next.Channel {
		return false
	}
	if first.Channel != "" {
		firstMeta, _ := parseChannelMeta(first.Prompt)
		nextMeta, _ := parseChannelMeta(next.Prompt)
		return sameQueuedChannelRoute(s.host.queuedChannelReply(first.Channel), firstMeta, nextMeta)
	}
	return true
}

// drainQueueForStep takes one ordered batch under the dispatch mutex. RunIDs
// do not split batches: completion is delivered to every original call.
func (s *sessionScheduler) drainQueueForStep(sessionID string) (fold, canceled []SessionAgentCall) {
	mu := s.sessionMu(sessionID)
	mu.Lock()
	defer mu.Unlock()
	return s.drainQueueForStepLocked(sessionID)
}

func (s *sessionScheduler) drainQueueForStepLocked(sessionID string) (fold, canceled []SessionAgentCall) {
	queued, _ := s.messageQueue.Get(sessionID)
	var keep []SessionAgentCall
	blocked := false
	for _, call := range queued {
		if s.canceledBySeq(sessionID, call.acceptSeq) || (call.sessionRun != nil && call.sessionRun.ctx.Err() != nil) {
			if len(call.batch) > 0 || call.RunID != "" || call.SubmissionID != "" || call.OnComplete != nil || call.queuedSessionRunRelease != nil {
				canceled = append(canceled, call)
			}
			continue
		}
		if len(fold) > 0 && !s.compatibleQueuedCalls(fold[0], call) {
			blocked = true
		}
		if blocked {
			keep = append(keep, call)
		} else {
			fold = append(fold, call)
		}
	}
	if len(keep) == 0 {
		s.messageQueue.Del(sessionID)
	} else {
		s.messageQueue.Set(sessionID, keep)
	}
	return fold, canceled
}

// reserveBatchLocked makes the whole drain observable to Cancel before Run
// registers its active request. A single fresh lease covers the entire batch;
// canceled pre-drain siblings are filtered using their original acceptSeq.
func (s *sessionScheduler) reserveBatchLocked(calls []SessionAgentCall) SessionAgentCall {
	call := calls[0]
	if len(calls) > 1 {
		call.batch = slices.Clone(calls)
		call.sessionRun = sharedBatchOwner(calls)
		call.queuedSessionRunRelease = nil
		call.OnComplete = nil
		call.Prompt = ""
		call.Attachments = nil
		var texts []string
		for _, original := range calls {
			texts = append(texts, message.PromptWithTextAttachments(original.Prompt, original.Attachments))
			for _, attachment := range original.Attachments {
				if !attachment.IsText() {
					call.Attachments = append(call.Attachments, attachment)
				}
			}
		}
		call.Prompt = strings.Join(texts, "\n\n")
	}
	call.acceptSeq = 0
	call.Accepted = s.beginAcceptedLocked(call.SessionID)
	// Cancellation here is covered by Accepted's high-water sequence. Run
	// observes that mark before consuming the lease or entering the model.
	call.batchReservation = &activeCancel{cancel: func() {}}
	s.activeRequests.Set(call.SessionID, call.batchReservation)
	return call
}

// finishDispatch runs after terminal publication, on every active Run exit.
// Interrupt owns its replacement queue until its waiting dispatcher takes it.
func (s *sessionScheduler) finishDispatch(ctx context.Context, sessionID string, ac *activeCancel) {
	mu := s.sessionMu(sessionID)
	mu.Lock()
	s.activeRequests.CompareAndDelete(sessionID, ac)
	handoff, interrupted := s.handoffs.Get(sessionID)
	if interrupted && handoff.reservation == ac && ac != nil {
		s.handoffs.Del(sessionID)
		interrupted = false
	}
	if interrupted || s.isRunning(sessionID) {
		if interrupted {
			handoff.signal()
		}
		mu.Unlock()
		return
	}
	// An accepted follow-up belongs to this drain even if its goroutine
	// has not entered Run yet. Close/validation failure releases the barrier.
	if s.hasAcceptedFollowUpsLocked(sessionID) {
		s.startQueueHandoffLocked(sessionID, false)
		mu.Unlock()
		return
	}
	calls, canceled := s.drainQueueForStepLocked(sessionID)
	var next SessionAgentCall
	if len(calls) > 0 {
		next = s.reserveBatchLocked(calls)
	} else {
		s.acceptedMu.Lock()
		inFlight, _ := s.acceptedRuns.Get(sessionID)
		if inFlight == 0 {
			s.cancelMark.Del(sessionID)
		}
		s.acceptedMu.Unlock()
	}
	mu.Unlock()
	s.publishCanceledQueueDrops(canceled)
	if len(calls) > 0 {
		// Each queued call carries its own owner and provenance. Do not
		// inherit the finished turn's sessionRun or submission context.
		if _, err := s.host.Run(context.Background(), next); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("Queued batch failed", "session_id", sessionID, "error", err)
		}
	}
}

// publishCanceledQueueDrops emits a terminal cancelled RunComplete for
// every dropped queued call that carries a RunID. A queued prompt removed
// from the queue without ever running — covered by a pending cancel, or
// cleared by Cancel/ClearQueue — would otherwise leave a caller blocked on
// that RunID: `crush run` ignores live message events and exits only on a
// RunComplete whose RunID matches. Calls without a RunID had no such waiter
// and are dropped silently as before. A detached, bounded context keeps the
// must-deliver publish alive even when the run context that triggered the
// drop is already canceled.
func (s *sessionScheduler) publishCanceledQueueDrops(drops []SessionAgentCall) {
	var needsCompletion bool
	for _, d := range drops {
		if len(d.batch) > 0 || d.RunID != "" || d.SubmissionID != "" || d.OnComplete != nil || d.queuedSessionRunRelease != nil {
			needsCompletion = true
			break
		}
	}
	if !needsCompletion {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, d := range drops {
		if len(d.batch) > 0 || d.RunID != "" || d.SubmissionID != "" || d.OnComplete != nil {
			s.host.publishRunComplete(ctx, d, notify.RunComplete{
				SessionID: d.SessionID,
				RunID:     d.RunID,
				Cancelled: true,
			})
		}
		releaseQueuedCall(d, context.Canceled)
	}
}

// clearQueueAndNotify removes all queued prompts for the session and
// publishes a terminal cancelled RunComplete for any that carried a RunID,
// so callers waiting on those RunIDs (e.g. `crush run`) are not left
// hanging when their queued prompt is discarded without running.
func (s *sessionScheduler) clearQueueAndNotify(sessionID string) {
	if h, ok := s.handoffs.Get(sessionID); ok {
		h.aborted = true
		s.cancelHandoffLeasesLocked(sessionID, h)
		h.signal()
	}
	queued, ok := s.messageQueue.Get(sessionID)
	s.messageQueue.Del(sessionID)
	if s, _ := s.steering.Get(sessionID); s != nil {
		queued = append(s.takePendingSteered(), queued...)
		ok = ok || len(queued) > 0
	}
	if pending, took := s.interrupting.Take(sessionID); took {
		queued, ok = append(pending, queued...), true
	}
	if !ok {
		return
	}
	s.publishCanceledQueueDrops(queued)
}

// clearPendingCancel removes any pending-cancel mark for sessionID. It
// takes the per-session dispatch lock so it is ordered against Cancel
// and the dispatch handoff.
func (s *sessionScheduler) clearPendingCancel(sessionID string) {
	mu := s.sessionMu(sessionID)
	mu.Lock()
	defer mu.Unlock()
	s.cancelMark.Del(sessionID)
}

// canceledBySeq reports whether an accepted handle or queued call with
// the given accept sequence is covered by a pending cancel for the
// session. Callers must hold the session's dispatch mutex. A tracked
// sequence (seq > 0) is covered only when it is at or below the cancel
// high-water mark, so a prompt accepted after the cancel (higher seq) is
// never poisoned. An untracked sequence (seq == 0, an in-process enqueue
// with no accept reservation) is covered whenever any mark is present,
// preserving the pre-sequence behavior. The mark is not consumed: it
// stays so every sibling handle it covers observes the same cancel, and
// a later handle (higher seq) ignores it regardless.
func (s *sessionScheduler) canceledBySeq(sessionID string, seq uint64) bool {
	mark, ok := s.cancelMark.Get(sessionID)
	if !ok || mark == 0 {
		return false
	}
	return seq == 0 || seq <= mark
}

func (s *sessionScheduler) Cancel(sessionID string) {
	// Serialize against the dispatch handoff in Run so the accepted ->
	// (cancel-on-entry | queued | active) transition is atomic against
	// this cancel. Every cancel observes at least one of: an active
	// request, an accepted run (recorded as a pending cancel), or a
	// queue entry it then clears. If none of those hold, an idle Escape
	// is a true no-op and must not poison the next prompt.
	mu := s.sessionMu(sessionID)
	mu.Lock()
	defer mu.Unlock()
	s.cancelLocked(sessionID)
}

// cancelLocked requires the session's dispatch mutex.
func (s *sessionScheduler) cancelLocked(sessionID string) {
	s.cancelActiveLocked(sessionID)

	// Record a pending cancel only when a dispatched-but-not-yet-active
	// run exists. This catches runs still in the goroutine scheduler or
	// about to enter Run's busy-queue branch, while leaving an idle
	// session untouched. Active and accepted are not mutually exclusive:
	// when a run is active and a follow-up has been accepted, both the
	// cancel above and this pending record fire.
	//
	// Raise the session's cancel mark to the latest accept sequence
	// assigned so far. Every prompt currently accepted-but-not-yet-
	// active has a sequence at or below that value, so one cancel covers
	// all of them; a prompt accepted after this cancel gets a strictly
	// higher sequence and is never poisoned. Using max keeps repeated
	// cancels idempotent while the same prompts are in flight and lets a
	// later cancel extend coverage to prompts accepted since.
	s.acceptedMu.Lock()
	count, ok := s.acceptedRuns.Get(sessionID)
	mark := s.acceptSeqGen
	s.acceptedMu.Unlock()
	if ok && count > 0 {
		slog.Debug("Recording cancel mark for accepted runs", "session_id", sessionID, "count", count, "mark", mark)
		existing, _ := s.cancelMark.Get(sessionID)
		s.cancelMark.Set(sessionID, max(existing, mark))
	}

	s.clearQueueAndNotify(sessionID)
}

// cancelActiveLocked requires the session dispatch mutex.
func (s *sessionScheduler) cancelActiveLocked(sessionID string) {
	// Cancel regular requests. Don't use Take() here - we need the entry to
	// remain in activeRequests so IsBusy() returns true until the goroutine
	// fully completes (including error handling that may access the DB).
	// The defer in processRequest will clean up the entry.
	if ac, ok := s.activeRequests.Get(sessionID); ok && ac != nil {
		slog.Debug("Request cancellation initiated", "session_id", sessionID)
		ac.cancel()
	}

	// Also check for summarize requests.
	if ac, ok := s.activeRequests.Get(sessionID + "-summarize"); ok && ac != nil {
		slog.Debug("Summarize cancellation initiated", "session_id", sessionID)
		ac.cancel()
	}
}

// Interrupt preserves follow-ups and returns immediately. Repeats while the
// canceled turn and its replacement exchange ownership are idempotent.
func (s *sessionScheduler) Interrupt(sessionID string) {
	mu := s.sessionMu(sessionID)
	mu.Lock()
	defer mu.Unlock()
	if _, transitioning := s.handoffs.Get(sessionID); transitioning {
		return
	}
	s.startQueueHandoffLocked(sessionID, true)
}

func (s *sessionScheduler) ClearQueue(sessionID string) {
	mu := s.sessionMu(sessionID)
	mu.Lock()
	defer mu.Unlock()
	s.clearQueueAndNotify(sessionID)
}

// RecallQueuedPrompt withdraws the newest prompt still owned by Crush.
// Prompts already handed to a CLI cannot be unsent, even before its echo.
func (s *sessionScheduler) RecallQueuedPrompt(sessionID string) *message.QueuedPrompt {
	mu := s.sessionMu(sessionID)
	mu.Lock()
	var recalled *SessionAgentCall
	for _, queue := range []*csync.Map[string, []SessionAgentCall]{s.messageQueue, s.interrupting} {
		calls, _ := queue.Get(sessionID)
		for i := len(calls) - 1; i >= 0; i-- {
			call := calls[i]
			if call.NotRecallable || call.HiddenUserMessage || call.CLIContinue || call.Channel != "" {
				continue
			}
			// Queue readers retain the old slice; publish a new one rather
			// than changing their backing array in place.
			calls = slices.Concat(calls[:i], calls[i+1:])
			if len(calls) == 0 {
				queue.Del(sessionID)
			} else {
				queue.Set(sessionID, calls)
			}
			recalled = &call
			break
		}
		if recalled != nil {
			break
		}
	}
	mu.Unlock()
	if recalled == nil {
		return nil
	}
	// Complete the original queued submission so headless callers and their
	// reservations cannot hang; a later editor submission is a new request.
	s.publishCanceledQueueDrops([]SessionAgentCall{*recalled})
	return &message.QueuedPrompt{Prompt: recalled.Prompt, Attachments: recalled.Attachments, SubmissionID: recalled.SubmissionID}
}

func (s *sessionScheduler) CancelAll() {
	if !s.IsBusy() {
		return
	}
	for key := range s.activeRequests.Seq2() {
		s.Cancel(key) // key is sessionID
	}

	timeout := time.After(5 * time.Second)
	for s.IsBusy() {
		select {
		case <-timeout:
			return
		default:
			time.Sleep(200 * time.Millisecond)
		}
	}
}

func (s *sessionScheduler) IsBusy() bool {
	busy := s.handoffs.Len() > 0 || s.interrupting.Len() > 0
	for ac := range s.activeRequests.Seq() {
		if ac != nil {
			busy = true
			break
		}
	}
	return busy
}

func (s *sessionScheduler) IsSessionBusy(sessionID string) bool {
	_, pending := s.interrupting.Get(sessionID)
	_, transitioning := s.handoffs.Get(sessionID)
	return s.isRunning(sessionID) || pending || transitioning
}

func (s *sessionScheduler) isRunning(sessionID string) bool {
	_, ok := s.activeRequests.Get(sessionID)
	return ok
}

func (s *sessionScheduler) QueuedPrompts(sessionID string) int {
	return len(s.QueuedPromptsList(sessionID))
}

// QueuedPromptsList lists the queued prompts, led by any an agent CLI has
// been handed but not taken in yet, so they stay visible until they land.
func (s *sessionScheduler) QueuedPromptsList(sessionID string) []string {
	var prompts []string
	if s, _ := s.steering.Get(sessionID); s != nil {
		prompts = s.pendingSteered()
	}
	pending, _ := s.interrupting.Get(sessionID)
	for _, call := range pending {
		prompts = append(prompts, call.Prompt)
	}
	l, _ := s.messageQueue.Get(sessionID)
	for _, call := range l {
		prompts = append(prompts, call.Prompt)
	}
	return prompts
}
