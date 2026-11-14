package agent

import (
	"context"
	"errors"
	"slices"
	"time"
)

// API helpers in the helper list: an API chat's subagents (agent_dispatch,
// and agent calls moved to the background) run in the coordinator, not in a
// task hub, so the app's Subagents list, tray and sheet never saw them.
// Here they are shown as tasks with API set, so the engine lists them,
// sends their helper events and stops them like `crush spawn` helpers.
// The terminal reads them from the chat itself and skips these.

// ChatHelpers returns a chat's helpers, running or ended: its `crush spawn`
// tasks and its API helpers, oldest first.
func ChatHelpers(sessionID string) []Task {
	out := SessionTasks(sessionID)
	for _, c := range hubCoordinators() {
		for _, entry := range c.dispatched.Seq2() {
			if entry.ParentSessionID == sessionID {
				out = append(out, c.dispatchTask(entry))
			}
		}
	}
	slices.SortFunc(out, func(a, b Task) int { return a.Started.Compare(b.Started) })
	return out
}

// StopChatHelper stops one of the chat's helpers for the user: a `crush
// spawn` task as StopSessionTask does, else an API helper by its ID (its
// own chat's ID), as agent_stop does.
func StopChatHelper(sessionID, id string) error {
	err := StopSessionTask(sessionID, id)
	if !errors.Is(err, ErrTaskNotInSession) {
		return err
	}
	for _, c := range hubCoordinators() {
		for _, entry := range c.dispatched.Seq2() {
			if entry.ParentSessionID == sessionID && entry.SessionID == id {
				entry.stop("stopped by the user")
				return nil
			}
		}
	}
	return ErrTaskNotInSession
}

// hubCoordinators lists the coordinators of this process's task hubs, once
// each.
func hubCoordinators() []*coordinator {
	hubsMu.Lock()
	defer hubsMu.Unlock()
	var out []*coordinator
	for _, h := range hubs {
		if h.c != nil && h.c.dispatched != nil && !slices.Contains(out, h.c) {
			out = append(out, h.c)
		}
	}
	return out
}

// dispatchTask is an API helper as a task. Its model is the one saved with
// its chat; before its first run saves one, the model is empty.
func (c *coordinator) dispatchTask(entry *dispatchedAgent) Task {
	snap := entry.snapshot()
	t := Task{
		ID:        entry.SessionID,
		SessionID: entry.ParentSessionID,
		ChildID:   entry.SessionID,
		Name:      entry.Label,
		ReadOnly:  entry.Access == accessRead,
		Started:   snap.Started,
		Ended:     snap.Ended,
		API:       true,
	}
	switch snap.State {
	case dispatchRunning:
		t.Status = TaskRunning
	case dispatchFailed:
		t.Status = TaskFailed
	case dispatchStopped:
		t.Status = TaskStopped
	default:
		t.Status = TaskDone
	}
	t.Delivered = t.Status != TaskRunning
	if c.sessions != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if sel, ok := c.chatSelection(ctx, entry.SessionID); ok {
			t.CLI, t.Model, t.Effort = sel.Provider, sel.Model, sel.ReasoningEffort
		}
	}
	return t
}

// publishDispatch sends an API helper's helper event, as a task hub does
// for its tasks.
func (c *coordinator) publishDispatch(entry *dispatchedAgent) {
	if c.tasks != nil {
		c.tasks.publish(c.dispatchTask(entry))
	}
}
