package agent

import (
	"log/slog"
	"slices"

	"github.com/charmbracelet/crush/internal/shell"
)

// Closing a chat that still works (owner, 2026-11-13): the close stops the
// chat for good. Its reply, every sub-agent it started, their own
// sub-agents, and every background job of those chats end, and no agent is
// told, so nothing starts a new turn in a chat that is gone.

// StopChatWork ends everything chat sessionID runs, as described above.
// A sub-agent's own chat is a chat too: its work and its sub-agents end.
func StopChatWork(c Coordinator, sessionID string) {
	if c == nil || sessionID == "" {
		return
	}
	co, _ := c.(*coordinator)
	// The reply first, so it starts no helper or job after the scan below.
	cancelTree := func(tree []string) {
		for _, id := range tree {
			c.Cancel(id)
			if co != nil {
				co.limitRetries.drop(id)
			}
		}
	}
	cancelTree([]string{sessionID})
	tree := chatTree(sessionID)
	hubsMu.Lock()
	listed := slices.Clone(hubs)
	hubsMu.Unlock()
	for _, h := range listed {
		h.stopTreeTasks(tree)
	}
	for _, id := range tree {
		stopChatJobs(id)
	}
	for _, h := range listed {
		h.forgetTree(tree)
	}
	// Every chat of the tree, after the helpers and jobs: a helper's chat
	// cancelled while its helper runs is the user's stop of it, which its
	// parent is told about. A result handed over just before the stop is
	// cut off too.
	cancelTree(tree)
	if co != nil {
		co.dropSavedWaits(tree)
	}
}

// chatTree lists sessionID and the chats of its sub-agents, theirs, and so
// on, ended ones included: an ended helper's own helper may still run.
func chatTree(sessionID string) []string {
	tree := []string{sessionID}
	for i := 0; i < len(tree); i++ {
		for _, t := range SessionTasks(tree[i]) {
			if t.ChildID != "" && !slices.Contains(tree, t.ChildID) {
				tree = append(tree, t.ChildID)
			}
		}
	}
	return tree
}

// stopTreeTasks stops the hub's running sub-agents of the chats in tree
// without telling their parents (stopForGood, for some chats only).
func (h *taskHub) stopTreeTasks(tree []string) {
	h.saveMu.Lock()
	defer h.saveMu.Unlock()
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, t := range h.tasks {
		if t.Status == TaskRunning && slices.Contains(tree, t.SessionID) {
			t.Status = TaskStopped
			h.cancels[id]()
		}
	}
}

// forgetTree drops every saved sub-agent and job of the chats in tree,
// ended, interrupted or released ones too, so no later engine resumes them
// or tells a closed chat about them.
func (h *taskHub) forgetTree(tree []string) {
	h.saveMu.Lock()
	defer h.saveMu.Unlock()
	if err := h.editSaved(func(saved []savedTask) []savedTask {
		return slices.DeleteFunc(saved, func(s savedTask) bool { return slices.Contains(tree, s.SessionID) })
	}); err != nil {
		slog.Warn("Failed to drop a closed chat's saved sub-agents", "error", err)
	}
	if err := h.editSavedJobs(func(saved []savedJob) []savedJob {
		return slices.DeleteFunc(saved, func(s savedJob) bool { return slices.Contains(tree, s.SessionID) })
	}); err != nil {
		slog.Warn("Failed to drop a closed chat's saved jobs", "error", err)
	}
}

// stopChatJobs ends chat sessionID's background jobs, services included,
// with no "job ended" turn and no saved entry for a later resume.
func stopChatJobs(sessionID string) {
	for _, p := range SessionBackgroundProcesses(sessionID) {
		var err error
		if p.JobID == "" {
			err = killProcess(p.PID, false)
		} else {
			err = dropJob(p.JobID)
		}
		if err != nil {
			slog.Warn("Failed to stop a closed chat's background job", "session", sessionID, "job", p.JobID, "pid", p.PID, "error", err)
		}
	}
}

// dropJob kills managed job id; its watcher forgets it without a turn
// (forgetTree drops its saved entry even when the watcher has gone).
func dropJob(id string) error {
	h := hubWithJob(id)
	if h == nil {
		return shell.GetBackgroundShellManager().Kill(id)
	}
	h.mu.Lock()
	if h.droppedJobs == nil {
		h.droppedJobs = make(map[string]bool)
	}
	h.droppedJobs[id] = true
	h.mu.Unlock()
	// Kill waits for the job to end, so its watcher sees the mark.
	if err := shell.GetBackgroundShellManager().Kill(id); err != nil {
		h.mu.Lock()
		delete(h.droppedJobs, id)
		h.mu.Unlock()
		return err
	}
	return nil
}

// dropSavedWaits drops the saved continuations (a usage limit's reset, the
// fallback order's waits) of the chats in tree.
func (c *coordinator) dropSavedWaits(tree []string) {
	err := c.updatePending(func(p *pendingWork) bool {
		left := slices.DeleteFunc(p.Waits, func(w savedWait) bool { return slices.Contains(tree, w.SessionID) })
		changed := len(left) != len(p.Waits)
		p.Waits = left
		return changed
	})
	if err != nil {
		slog.Warn("Failed to drop a closed chat's planned waits", "error", err)
	}
}
