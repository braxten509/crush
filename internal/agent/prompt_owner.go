package agent

import "github.com/charmbracelet/crush/internal/csync"

// Prompts of an app that leaves (engine-design.md 5.5). The backend knows
// which app sent each prompt by its submission ID; StopOwned ends one app's
// prompts in one chat and leaves every other app's alone.

// OwnedStopper is implemented by the coordinator. The backend type-asserts
// it, the way QueueSummaries does, so the Coordinator interface and its
// mocks stay unchanged.
type OwnedStopper interface {
	StopOwned(sessionID string, owned func(submissionID string) bool, ownsTurn func(queued []string) bool) int
}

// StopOwned ends a leaving app's prompts in one chat. It runs under the
// session's dispatch mutex, so no other prompt can slip in between its
// steps. It never cancels: a cancel raises the cancel mark over every
// accepted prompt that has not reached the scheduler, and those carry no
// submission ID, so a cancel would also end another app's prompt accepted at
// that moment.
//  1. It drops the waiting prompts whose submission ID makes owned return
//     true, from the queue and from an interrupt's snapshot, and publishes
//     their cancelled RunComplete the way ClearQueue does. The other prompts
//     keep their order.
//  2. It ends the running turn only when a turn is active, no queue handoff
//     is in progress, no accepted prompt waits to start a new turn (an
//     interrupt cancels such a primary acceptance, and its app is unknown),
//     and ownsTurn(queued) returns true. queued holds the submission IDs
//     QueuedPromptSummaries listed before step 1, so the prompts step 1
//     drops count as waiting, never as running. It then interrupts, as
//     Interrupt does, which moves the prompts handed to an agent CLI and the
//     queue into the interrupt's snapshot, and drops the owned ones from it
//     again before it unlocks. Accepted follow-ups keep their place, so
//     another app's prompt accepted a moment earlier still runs next.
//
// It returns how many prompts it dropped.
func (s *sessionScheduler) StopOwned(sessionID string, owned func(submissionID string) bool, ownsTurn func(queued []string) bool) int {
	mu := s.sessionMu(sessionID)
	mu.Lock()
	var queued []string
	for _, q := range s.QueuedPromptSummaries(sessionID) {
		queued = append(queued, q.SubmissionID)
	}
	drops := s.dropOwnedLocked(s.messageQueue, sessionID, owned)
	drops = append(drops, s.dropOwnedLocked(s.interrupting, sessionID, owned)...)
	_, transitioning := s.handoffs.Get(sessionID)
	if s.isRunning(sessionID) && !transitioning && !s.hasAcceptedPrimaryLocked(sessionID) && ownsTurn(queued) {
		s.startQueueHandoffLocked(sessionID, true)
		drops = append(drops, s.dropOwnedLocked(s.interrupting, sessionID, owned)...)
	}
	mu.Unlock()
	s.publishCanceledQueueDrops(drops)
	n := 0
	for _, d := range drops {
		n += max(1, len(d.batch))
	}
	return n
}

// dropOwnedLocked takes the owned calls out of the session's entry in queue
// and keeps the others in order. It requires the dispatch mutex.
func (s *sessionScheduler) dropOwnedLocked(queue *csync.Map[string, []SessionAgentCall], sessionID string, owned func(string) bool) []SessionAgentCall {
	calls, _ := queue.Get(sessionID)
	var kept, drops []SessionAgentCall
	for _, call := range calls {
		if ownedCall(call, owned) {
			drops = append(drops, call)
		} else {
			kept = append(kept, call)
		}
	}
	if len(drops) == 0 {
		return nil
	}
	// Queue readers keep the old slice: publish a new one. An emptied
	// snapshot leaves its handoff in place, which dispatches nothing.
	if len(kept) == 0 {
		queue.Del(sessionID)
	} else {
		queue.Set(sessionID, kept)
	}
	return drops
}

// ownedCall reports a call whose prompts are all owned; a batch is owned only
// when every prompt in it is.
func ownedCall(call SessionAgentCall, owned func(string) bool) bool {
	if len(call.batch) == 0 {
		return call.SubmissionID != "" && owned(call.SubmissionID)
	}
	for _, original := range call.batch {
		if original.SubmissionID == "" || !owned(original.SubmissionID) {
			return false
		}
	}
	return true
}

// hasAcceptedPrimaryLocked reports an accepted prompt that has not reached
// the scheduler and would start a new turn of its own: an interrupt would
// cancel it. It requires the dispatch mutex.
func (s *sessionScheduler) hasAcceptedPrimaryLocked(sessionID string) bool {
	s.acceptedMu.Lock()
	defer s.acceptedMu.Unlock()
	for _, r := range s.acceptedLeases[sessionID] {
		if !r.followUp && !r.done.Load() && !r.canceled.Load() && !s.canceledBySeq(sessionID, r.seq) {
			return true
		}
	}
	return false
}

// StopOwned ends a leaving app's prompts in one chat on the current agent;
// see sessionScheduler.StopOwned.
func (c *coordinator) StopOwned(sessionID string, owned func(string) bool, ownsTurn func([]string) bool) int {
	if stopper, ok := c.currentAgent().(OwnedStopper); ok {
		return stopper.StopOwned(sessionID, owned, ownsTurn)
	}
	return 0
}
