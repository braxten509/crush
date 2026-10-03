package agent

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"
)

// queueHandoff owns one atomic queue snapshot until its replacement registers.
// pending is guarded by acceptedMu; aborted and reservation by dispatchMu.
// Acceptances after the snapshot remain in the later queue.
type queueHandoff struct {
	pending     int
	wake        chan struct{}
	aborted     bool
	reservation *activeCancel
}

func (h *queueHandoff) signal() {
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

// Publish a new slice: concurrent queue displays can still own the old one.
func appendQueuedInOrder(calls []SessionAgentCall, call SessionAgentCall) []SessionAgentCall {
	ordered := append(slices.Clone(calls), call)
	slices.SortStableFunc(ordered, func(first, next SessionAgentCall) int {
		return cmp.Compare(first.queueOrder, next.queueOrder)
	})
	return ordered
}

func (a *sessionAgent) hasAcceptedFollowUpsLocked(sessionID string) bool {
	a.acceptedMu.Lock()
	defer a.acceptedMu.Unlock()
	for _, r := range a.acceptedLeases[sessionID] {
		if r.followUp && !r.done.Load() && !r.canceled.Load() && !a.canceledBySeq(sessionID, r.seq) {
			return true
		}
	}
	return false
}

// startQueueHandoffLocked requires dispatchMu. Unlike Cancel, Interrupt
// cancels only primary acceptances; accepted follow-ups retain queue ownership.
func (a *sessionAgent) startQueueHandoffLocked(sessionID string, interrupt bool) {
	h := &queueHandoff{wake: make(chan struct{}, 1)}
	calls, _ := a.interrupting.Take(sessionID)
	if interrupt {
		if s, _ := a.steering.Get(sessionID); s != nil {
			calls = append(calls, s.takePendingSteered()...)
		}
	}
	queued, _ := a.messageQueue.Take(sessionID)
	calls = append(calls, queued...)
	a.acceptedMu.Lock()
	for _, r := range a.acceptedLeases[sessionID] {
		if r.done.Load() || r.canceled.Load() || a.canceledBySeq(sessionID, r.seq) {
			continue
		}
		if r.followUp {
			r.handoff = h
			h.pending++
		} else if interrupt {
			r.canceled.Store(true)
		}
	}
	pending := h.pending
	a.acceptedMu.Unlock()
	if interrupt {
		a.cancelActiveLocked(sessionID)
	}
	if len(calls) == 0 && pending == 0 {
		return
	}
	a.interrupting.Set(sessionID, calls)
	a.handoffs.Set(sessionID, h)
	go a.dispatchQueueHandoff(sessionID, h)
}

func (a *sessionAgent) cancelHandoffLeasesLocked(sessionID string, h *queueHandoff) {
	a.acceptedMu.Lock()
	defer a.acceptedMu.Unlock()
	for _, r := range a.acceptedLeases[sessionID] {
		if r.handoff == h {
			r.canceled.Store(true)
		}
	}
}

func (a *sessionAgent) dispatchQueueHandoff(sessionID string, h *queueHandoff) {
	mu := a.sessionMu(sessionID)
	deadline := time.Now().Add(time.Minute)
	// Real Run cleanup and lease Close wake us immediately. The ticker
	// also supports cancellation entries removed by other cleanup paths.
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		mu.Lock()
		current, _ := a.handoffs.Get(sessionID)
		if current != h {
			mu.Unlock()
			return
		}
		a.acceptedMu.Lock()
		pending := h.pending
		a.acceptedMu.Unlock()
		if !a.isRunning(sessionID) && (h.aborted || pending == 0) {
			break
		}
		if time.Now().After(deadline) {
			a.cancelHandoffLeasesLocked(sessionID, h)
			drops, _ := a.interrupting.Take(sessionID)
			a.handoffs.Del(sessionID)
			mu.Unlock()
			a.publishCanceledQueueDrops(drops)
			slog.Error("Queue handoff did not finish; retained prompts canceled", "session_id", sessionID)
			// If only a lease stalled, later arrivals can still run.
			a.finishDispatch(context.Background(), sessionID, nil)
			return
		}
		mu.Unlock()
		select {
		case <-h.wake:
		case <-ticker.C:
		}
	}
	calls, _ := a.interrupting.Take(sessionID)
	calls = slices.Clone(calls)
	// Accepted calls can enter Run out of order. Their acceptance sequence
	// places them among the already queued/steered originals in this snapshot.
	slices.SortStableFunc(calls, func(first, next SessionAgentCall) int {
		return cmp.Compare(first.queueOrder, next.queueOrder)
	})
	var kept, canceled []SessionAgentCall
	for _, call := range calls {
		if h.aborted || a.canceledBySeq(sessionID, call.acceptSeq) || (call.sessionRun != nil && call.sessionRun.ctx.Err() != nil) {
			canceled = append(canceled, call)
			continue
		}
		kept = append(kept, call)
	}
	if len(kept) == 0 {
		a.handoffs.Del(sessionID)
		mu.Unlock()
		a.publishCanceledQueueDrops(canceled)
		// Recall, validation failure or Close can leave an empty snapshot.
		// Dispatch later arrivals without inheriting the finished turn's owner.
		a.finishDispatch(context.Background(), sessionID, nil)
		return
	}
	count := 1
	for count < len(kept) && a.compatibleQueuedCalls(kept[0], kept[count]) {
		count++
	}
	if count < len(kept) {
		a.requeueFrontLocked(sessionID, kept[count:])
	}
	next := a.reserveBatchLocked(kept[:count])
	h.reservation = next.batchReservation
	next.handoff = h
	// Keep the handoff marker until Run atomically registers its active
	// cancellation handle. A repeat Interrupt in this window is a no-op.
	mu.Unlock()
	a.publishCanceledQueueDrops(canceled)
	if _, err := a.Run(context.Background(), next); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("Queued batch after handoff failed", "session_id", sessionID, "error", err)
	}
}
