package agent

import (
	"cmp"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"charm.land/catwalk/pkg/catwalk"

	"github.com/charmbracelet/crush/internal/config"
)

// Follow-ups to finished sub-agents. A finished task keeps its child
// session, and the agent CLI's own conversation stays linked to it, so a
// follow-up runs there and the sub-agent remembers its earlier work. Tasks
// are listed in the project's data directory, so their IDs keep working
// after Crush restarts.

const taskHistoryFile = "task-history.json"

// taskHistoryLimit bounds the list; the oldest tasks drop off first.
const taskHistoryLimit = 500

const continuePreamble = `This is a follow-up from the agent that started you. Your earlier work in this conversation is still yours: build on it instead of starting over, and check the current state of the files first. When you're done, end with a concise report, as before.

`

func (h *taskHub) historyPath() string {
	path := h.savedTasksPath()
	if path == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(path), taskHistoryFile)
}

// recordHistory lists a task so it can be continued later.
func (h *taskHub) recordHistory(entry savedTask) {
	err := editTaskFile(h.historyPath(), func(saved []savedTask) []savedTask {
		saved = slices.DeleteFunc(saved, func(s savedTask) bool {
			return s.ChildID == entry.ChildID || s.SessionID == entry.SessionID && s.ID == entry.ID
		})
		saved = append(saved, entry)
		if len(saved) > taskHistoryLimit {
			saved = saved[len(saved)-taskHistoryLimit:]
		}
		return saved
	})
	if err != nil {
		slog.Warn("Failed to list a sub-agent", "task", entry.ID, "error", err)
	}
}

// history reads the listed tasks of a session.
func (h *taskHub) history(sessionID string) []savedTask {
	var out []savedTask
	_ = editTaskFile(h.historyPath(), func(saved []savedTask) []savedTask {
		for _, s := range saved {
			if s.SessionID == sessionID {
				out = append(out, s)
			}
		}
		return saved
	})
	return out
}

// skipUsedIDs moves the task counter past the IDs an earlier Crush gave
// this session's tasks, so a new task never takes an old one's ID.
func (h *taskHub) skipUsedIDs(sessionID string) {
	highest := 0
	for _, s := range h.history(sessionID) {
		if n, ok := taskNumber(s.ID); ok && n > highest {
			highest = n
		}
	}
	h.mu.Lock()
	h.seq = max(h.seq, highest)
	h.mu.Unlock()
}

// findTask looks a session's task up by ID, in this Crush or the list.
func (h *taskHub) findTask(sessionID, id string) (Task, bool) {
	h.mu.Lock()
	t, ok := h.tasks[id]
	if ok && t.SessionID == sessionID {
		found := *t
		h.mu.Unlock()
		return found, true
	}
	h.mu.Unlock()
	for _, s := range slices.Backward(h.history(sessionID)) {
		if s.ID == id {
			return s.Task, true
		}
	}
	return Task{}, false
}

// continueTask runs a follow-up on a finished task's child session.
func (h *taskHub) continueTask(req TaskRequest) (*Task, error) {
	ctx, release := h.reserveFollowUp(req.Session)
	started := false
	defer func() {
		if !started {
			release()
		}
	}()
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return nil, errors.New("the follow-up prompt is empty")
	}
	prev, ok := h.findTask(req.Session, req.Continue)
	if !ok {
		return nil, fmt.Errorf("no task %q in this session", req.Continue)
	}
	if h.childRunning(prev.ChildID) {
		return nil, fmt.Errorf("task %s is still running; its result will arrive as a <%s> message, so continue it after that", prev.ID, TaskNotificationTag)
	}
	if _, err := h.c.sessions.Get(ctx, prev.ChildID); err != nil {
		return nil, fmt.Errorf("task %s's conversation is gone, so start a new task instead", prev.ID)
	}
	provider, model, err := h.taskTarget(prev)
	if err != nil {
		return nil, err
	}
	tuning := TaskRequest{Effort: cmp.Or(req.Effort, prev.Effort), Fast: req.Fast || prev.Fast}
	selected, err := taskModel(provider, model, tuning)
	if err != nil {
		return nil, err
	}
	readOnly := prev.ReadOnly || req.ReadOnly
	sub, m, err := h.subAgent(ctx, provider, model, selected, readOnly)
	if err != nil {
		return nil, err
	}
	if !config.IsCLIProviderType(provider.Type) {
		h.c.permissions.AutoApproveSession(prev.ChildID)
	}

	t := prev
	t.Status, t.Started, t.Ended, t.Delivered = TaskRunning, time.Now(), time.Time{}, false
	t.Effort, t.Fast, t.ReadOnly = selected.ReasoningEffort, tuning.Fast, readOnly
	h.mu.Lock()
	if other, taken := h.tasks[t.ID]; taken && other.ChildID != t.ChildID {
		t.ID = "" // a newer task has its ID; give it the next one
	} else if n, ok := taskNumber(t.ID); ok && n > h.seq {
		h.seq = n
	}
	h.mu.Unlock()
	started = true
	snapshot := h.start(ctx, release, &t, sub, m, provider, continuePreamble+prompt)
	return &snapshot, nil
}

// childRunning reports whether a task on this child session is running.
func (h *taskHub) childRunning(childID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, t := range h.tasks {
		if t.ChildID == childID && t.Status == TaskRunning {
			return true
		}
	}
	return false
}

// taskTarget finds the provider and model a task ran on.
func (h *taskHub) taskTarget(t Task) (config.ProviderConfig, catwalk.Model, error) {
	for _, p := range h.taskProviders() {
		if !strings.EqualFold(taskProviderName(p), t.CLI) {
			continue
		}
		i := slices.IndexFunc(p.Models, func(m catwalk.Model) bool { return m.ID == t.Model })
		if i < 0 {
			return config.ProviderConfig{}, catwalk.Model{}, fmt.Errorf("model %q is no longer available on %s", t.Model, t.CLI)
		}
		return p, p.Models[i], nil
	}
	return config.ProviderConfig{}, catwalk.Model{}, fmt.Errorf("%s is no longer available", t.CLI)
}
