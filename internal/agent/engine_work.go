package agent

import (
	"log/slog"
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
	helpers += co.runningDispatchCount()
	return len(co.busySessions()), helpers
}

// WaitingChats counts c's chats waiting for a usage limit to reset, to be
// continued then. They keep a closed desktop's engine running like work
// does (owner, 2026-11-09), so the chat goes on at the reset.
func WaitingChats(c Coordinator) int {
	co, ok := c.(*coordinator)
	if !ok || co == nil {
		return 0
	}
	return co.limitRetries.count()
}

// StopAllWork ends the engine's running turns and helpers before it exits.
// Refill waits for open chats are saved for the next engine.
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
	co.releaseOpenWaits()
	for _, id := range co.busySessions() {
		c.Cancel(id)
	}
}

// Stop local timers as the engine exits. Refill waits for chats the user
// kept open belong to the next engine. Other stopped work ends here.
func (c *coordinator) releaseOpenWaits() {
	c.limitRetries.stopAll()
	self := os.Getpid()
	err := c.updatePending(func(p *pendingWork) bool {
		changed := false
		kept := p.Waits[:0]
		for _, w := range p.Waits {
			if w.Owner == self {
				changed = true
				if !w.waitsForRefill() || !chatIsOpen(w.SessionID) {
					continue
				}
				w.Owner, w.OpenChatOnly = 0, true
			}
			kept = append(kept, w)
		}
		p.Waits = kept
		return changed
	})
	if err != nil {
		slog.Warn("Failed to release planned waits", "error", err)
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
