package agent

import (
	"log/slog"
	"maps"
	"os"
	"slices"
	"strings"
)

// What an engine runs when the desktop's last window closes, and the stop
// that ends it all (owner, 2026-10-27: the close question offers to keep the
// engine running or to stop everything).

// RunningWork counts c's chats with a turn running (a helper's own chat is
// not one) and its running sub-agents. A coordinator of another kind (test
// mocks) counts as one chat while it is busy.
func RunningWork(c Coordinator) (chats, helpers int) {
	if c == nil {
		return 0, 0
	}
	co, ok := c.(*coordinator)
	if !ok {
		if c.IsBusy() {
			return 1, 0
		}
		return 0, 0
	}
	if co.tasks != nil {
		helpers = co.tasks.runningCount()
	}
	return len(co.busySessions()), helpers
}

// StopAllWork ends every chat turn and sub-agent of c for good, as the
// user's stop does, before the engine exits. A shutdown saves the helpers it
// cuts off and the chats waiting for a usage limit to reset, for the next
// engine to continue; these are dropped instead, and no parent is told that
// its helper ended.
func StopAllWork(c Coordinator) {
	if c == nil {
		return
	}
	co, ok := c.(*coordinator)
	if !ok {
		c.CancelAll()
		return
	}
	if co.tasks != nil {
		co.tasks.stopForGood()
	}
	for _, id := range co.busySessions() {
		c.Cancel(id)
	}
	co.dropWaits()
}

// dropWaits drops the chats' planned continuations (a usage limit's reset,
// the fallback order's waits) and their saved entries, so no later engine
// continues them.
func (c *coordinator) dropWaits() {
	c.limitRetries.mu.Lock()
	planned := slices.Collect(maps.Keys(c.limitRetries.pending))
	c.limitRetries.mu.Unlock()
	for _, id := range planned {
		c.limitRetries.drop(id)
	}
	self := os.Getpid()
	err := c.updatePending(func(p *pendingWork) bool {
		left := slices.DeleteFunc(p.Waits, func(w savedWait) bool { return w.Owner == self })
		changed := len(left) != len(p.Waits)
		p.Waits = left
		return changed
	})
	if err != nil {
		slog.Warn("Failed to drop the planned waits", "error", err)
	}
}

// busySessions lists the chats whose turn runs on the main agent, waits
// through an interrupt or hands its queue over: IsSessionBusy's chats.
func (c *coordinator) busySessions() []string {
	a, ok := c.currentAgent().(*sessionAgent)
	if !ok {
		return nil
	}
	var out []string
	add := func(id string) {
		// A summary runs under "<chat>-summarize".
		id = strings.TrimSuffix(id, "-summarize")
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	for id, ac := range a.activeRequests.Seq2() {
		if ac != nil {
			add(id)
		}
	}
	for id := range a.interrupting.Seq2() {
		add(id)
	}
	for id := range a.handoffs.Seq2() {
		add(id)
	}
	slices.Sort(out)
	return out
}

// runningCount counts the hub's running sub-agents.
func (h *taskHub) runningCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, t := range h.tasks {
		if t.Status == TaskRunning {
			n++
		}
	}
	return n
}

// stopForGood stops every running sub-agent without telling its parent, and
// drops its saved entry so no later engine resumes it. The hub closes first,
// so no sub-agent starts after the scan; one starting meanwhile is either
// saved before the scan's forget or not at all (saveMu, startRun).
func (h *taskHub) stopForGood() {
	h.saveMu.Lock()
	defer h.saveMu.Unlock()
	h.mu.Lock()
	h.closing = true
	var children []string
	for id, t := range h.tasks {
		if t.Status == TaskRunning {
			t.Status = TaskStopped
			h.cancels[id]()
			children = append(children, t.ChildID)
		}
	}
	h.mu.Unlock()
	for _, child := range children {
		h.forget(child)
	}
}
