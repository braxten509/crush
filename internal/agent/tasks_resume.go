package agent

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/catwalk/pkg/catwalk"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/lock"
)

// Interrupted sub-agents. An interactive Crush saves each running task in
// the project's data directory until the task ends. When Crush quits or
// crashes with tasks still running, the next Crush in that project starts
// them again on their own child sessions, so the agent CLI picks up its own
// conversation, and the parent still gets the result under the task's ID.

const savedTasksFile = "tasks.json"

// resumeWindow is how long an interrupted task stays worth resuming; older
// work is likely stale against the files and the parent conversation.
var resumeWindow = 24 * time.Hour

const resumePrompt = `Crush restarted while you were working, which cut off your last turn. Continue the same task from where you left off. Check the current state of the files first, since some of your changes may already be in place, and don't redo finished work. When you're done, end with the concise report described in your task.`

type savedTask struct {
	Task
	// Provider is the provider ID the task runs on.
	Provider string `json:"provider"`
	// Owner is the process ID of the Crush running the task.
	Owner int `json:"owner"`
	// Interrupted is when Crush shut down with the task running; it stays
	// zero after a crash.
	Interrupted time.Time `json:"interrupted,omitzero"`
}

// ResumeInterruptedTasks starts again the tasks an earlier Crush in each
// project was running when it quit or crashed, and tells each agent which of
// its background jobs that Crush cut off. Callers run it once the UI listens
// for task events, so resumed tasks show up like new ones.
func ResumeInterruptedTasks() {
	hubsMu.Lock()
	pending := slices.Clone(hubs)
	hubsMu.Unlock()
	for _, h := range pending {
		h.resumeOnce.Do(func() { go h.resumeInterrupted(context.Background()) })
	}
}

// savedTasksPath is where this hub saves its running tasks, or "" when it
// doesn't: non-interactive runs end with their tasks.
func (h *taskHub) savedTasksPath() string {
	return h.savedDataPath(savedTasksFile)
}

// savedDataPath is where this hub saves the named file of running work, or
// "" when it doesn't: non-interactive runs end with their work.
func (h *taskHub) savedDataPath(name string) string {
	if h.c == nil || h.c.cfg == nil || !h.c.interactive {
		return ""
	}
	dir := h.c.cfg.Config().Options.DataDirectory
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, name)
}

// editSaved changes the saved tasks under a lock shared by every Crush in
// the project.
func (h *taskHub) editSaved(edit func([]savedTask) []savedTask) error {
	return editTaskFile(h.savedTasksPath(), edit)
}

// editTaskFile changes a list saved at path under a lock shared by every
// Crush in the project. An empty path saves nothing.
func editTaskFile[T any](path string, edit func([]T) []T) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	release, err := lock.File(ctx, path+".lock")
	if err != nil {
		return err
	}
	defer release()

	var saved []T
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return err
	default:
		if err := json.Unmarshal(data, &saved); err != nil {
			slog.Warn("Discarding an unreadable saved list", "path", path, "error", err)
			saved = nil
		}
	}
	saved = edit(saved)
	if len(saved) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	out, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// remember saves a running task, and lists it so it can be continued later.
func (h *taskHub) remember(t Task, providerID string) {
	entry := savedTask{Task: t, Provider: providerID, Owner: os.Getpid()}
	err := h.editSaved(func(saved []savedTask) []savedTask {
		saved = slices.DeleteFunc(saved, func(s savedTask) bool { return s.ChildID == t.ChildID })
		return append(saved, entry)
	})
	if err != nil {
		slog.Warn("Failed to save a running sub-agent", "task", t.ID, "error", err)
	}
	h.recordHistory(entry)
}

// forget drops a task that ended.
func (h *taskHub) forget(childID string) {
	err := h.editSaved(func(saved []savedTask) []savedTask {
		return slices.DeleteFunc(saved, func(s savedTask) bool { return s.ChildID == childID })
	})
	if err != nil {
		slog.Warn("Failed to drop a finished sub-agent", "child_session", childID, "error", err)
	}
}

// markInterrupted records when shutdown cut these tasks off.
func (h *taskHub) markInterrupted(childIDs []string) {
	if len(childIDs) == 0 {
		return
	}
	now := time.Now()
	err := h.editSaved(func(saved []savedTask) []savedTask {
		for i := range saved {
			if slices.Contains(childIDs, saved[i].ChildID) {
				saved[i].Interrupted = now
			}
		}
		return saved
	})
	if err != nil {
		slog.Warn("Failed to mark interrupted sub-agents", "error", err)
	}
}

// claimInterrupted takes over the saved tasks whose Crush is gone and drops
// the ones too old to resume. Tasks of a Crush that still runs, this one
// included, are left alone.
func (h *taskHub) claimInterrupted() ([]savedTask, error) {
	self := os.Getpid()
	var claimed []savedTask
	err := h.editSaved(func(saved []savedTask) []savedTask {
		kept := saved[:0]
		for _, s := range saved {
			switch {
			case s.Owner == self || taskProcessAlive(s.Owner):
				kept = append(kept, s)
			case time.Since(cmp.Or(s.Interrupted, s.Started)) > resumeWindow:
				slog.Info("Not resuming an old interrupted sub-agent", "task", s.ID, "name", s.Name)
			default:
				s.Owner = self
				claimed = append(claimed, s)
				kept = append(kept, s)
			}
		}
		return kept
	})
	return claimed, err
}

func (h *taskHub) resumeInterrupted(ctx context.Context) {
	claimed, err := h.claimInterrupted()
	if err != nil {
		slog.Warn("Failed to read interrupted sub-agents", "error", err)
	}
	for _, s := range claimed {
		if err := h.resume(ctx, s); err != nil {
			slog.Warn("Failed to resume an interrupted sub-agent", "task", s.ID, "name", s.Name, "error", err)
			h.forget(s.ChildID)
			continue
		}
		slog.Info("Resumed an interrupted sub-agent", "task", s.ID, "name", s.Name)
	}
	h.reportInterruptedJobs(ctx)
}

// resume runs a saved task again on its child session.
func (h *taskHub) resume(ctx context.Context, s savedTask) error {
	providers := h.taskProviders()
	i := slices.IndexFunc(providers, func(p config.ProviderConfig) bool { return p.ID == s.Provider })
	if i < 0 {
		return fmt.Errorf("provider %q is no longer available", s.Provider)
	}
	provider := providers[i]
	j := slices.IndexFunc(provider.Models, func(m catwalk.Model) bool { return m.ID == s.Model })
	if j < 0 {
		return fmt.Errorf("model %q is no longer available", s.Model)
	}
	model := provider.Models[j]
	if _, err := h.c.sessions.Get(ctx, s.SessionID); err != nil {
		return fmt.Errorf("parent session: %w", err)
	}
	if _, err := h.c.sessions.Get(ctx, s.ChildID); err != nil {
		return fmt.Errorf("task session: %w", err)
	}
	selected, err := taskModel(provider, model, TaskRequest{Effort: s.Effort, Fast: s.Fast})
	if err != nil {
		// The model's options changed since; run on its defaults.
		if selected, err = taskModel(provider, model, TaskRequest{}); err != nil {
			return err
		}
	}
	sub, m, err := h.subAgent(ctx, provider, model, selected)
	if err != nil {
		return err
	}
	if !config.IsCLIProviderType(provider.Type) {
		h.c.permissions.AutoApproveSession(s.ChildID)
	}

	t := s.Task
	t.Status = TaskRunning
	t.Ended = time.Time{}
	t.Effort = selected.ReasoningEffort
	h.mu.Lock()
	if _, taken := h.tasks[t.ID]; taken {
		t.ID = "" // another resumed task already has it
	} else if n, ok := taskNumber(t.ID); ok && n > h.seq {
		h.seq = n
	}
	h.mu.Unlock()
	h.start(ctx, func() {}, &t, sub, m, provider, resumePrompt)
	return nil
}

// taskNumber reads the number of a task ID like "t12".
func taskNumber(id string) (int, bool) {
	digits, ok := strings.CutPrefix(id, "t")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(digits)
	return n, err == nil
}
