package agent

import (
	"fmt"
	"slices"
	"sync"

	"github.com/charmbracelet/crush/internal/shell"
)

// Ownership of background work (engine-design.md 5.5). An engine that
// serves several apps records which app owns each helper and managed job,
// so that when one app leaves, only its own work stops. The terminal app
// installs no hook, so there every owner is "".

// WorkKind is the kind of background work a WorkStart is about.
type WorkKind string

const (
	WorkHelper WorkKind = "helper"
	WorkJob    WorkKind = "job"
)

// WorkStart is what the work-start hook learns about work that starts.
type WorkStart struct {
	Kind      WorkKind
	ID        string // task ID or managed job ID
	SessionID string // the chat it works for
	Resumed   bool   // a helper restarted by ResumeInterruptedTasks
}

var workHook struct {
	sync.Mutex
	fn func(WorkStart) string
}

// SetWorkStartHook installs fn, called synchronously when a helper or managed
// job starts, before the first event about it. fn returns the owning app's
// client ID ("" for none), stored as Task.StartedBy or the job's owner.
// nil uninstalls it. The terminal app never installs one.
func SetWorkStartHook(fn func(WorkStart) string) {
	workHook.Lock()
	defer workHook.Unlock()
	workHook.fn = fn
}

// workStarted asks the hook, if one is installed, for the owner of work that
// starts. Callers hold no hub lock: the hook reads the agent's queue.
func workStarted(w WorkStart) string {
	workHook.Lock()
	fn := workHook.fn
	workHook.Unlock()
	if fn == nil {
		return ""
	}
	return fn(w)
}

// SetTaskStartedBy changes a helper's owner and publishes an updated event.
func SetTaskStartedBy(id, clientID string) {
	h := hubWithTask(id)
	if h == nil {
		return
	}
	h.mu.Lock()
	t, ok := h.tasks[id]
	var snapshot Task
	if ok {
		t.StartedBy = clientID
		snapshot = *t
	}
	h.mu.Unlock()
	if ok {
		h.publish(snapshot)
	}
}

// InterruptTask ends a running helper the way shutdown does: it stays saved,
// marked interrupted and released (Owner 0), so the next
// ResumeInterruptedTasks in any Crush, this one included, starts it again.
// The parent is not told. An ended helper is a no-op.
func InterruptTask(id string) error {
	h := hubWithTask(id)
	if h == nil {
		return fmt.Errorf("no task %q", id)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	t := h.tasks[id]
	if t == nil || t.Status != TaskRunning {
		return nil
	}
	t.Status = TaskStopped
	if h.interrupting == nil {
		h.interrupting = make(map[string]bool)
	}
	h.interrupting[id] = true
	h.cancels[id]()
	// Under h.mu, as stopAll does: the release and resume's claim never
	// interleave.
	h.releaseInterrupted([]string{t.ChildID})
	return nil
}

// InterruptJob ends a listed job the way shutdown does. A managed job is
// killed and saved as interrupted and released, with no "job ended" turn now;
// resume tells the agent, as after a restart. Any other listed process is
// killed, and its end is not reported to the agent.
func InterruptJob(p Process) error {
	if p.JobID == "" {
		markInterruptedDetached(p.PID)
		return KillProcess(p.PID)
	}
	h := hubWithJob(p.JobID)
	if h == nil {
		return fmt.Errorf("background job %q is no longer running", p.JobID)
	}
	h.mu.Lock()
	if h.interrupting == nil {
		h.interrupting = make(map[string]bool)
	}
	h.interrupting[p.JobID] = true
	h.mu.Unlock()
	// Kill waits for the job to end, so its watcher sees the mark.
	if err := shell.GetBackgroundShellManager().Kill(p.JobID); err != nil {
		h.mu.Lock()
		delete(h.interrupting, p.JobID)
		h.mu.Unlock()
		return err
	}
	return nil
}

// hubWithTask finds the listed hub running task id, or else one that ran it.
func hubWithTask(id string) *taskHub {
	hubsMu.Lock()
	defer hubsMu.Unlock()
	var ended *taskHub
	for _, h := range hubs {
		h.mu.Lock()
		t, ok := h.tasks[id]
		running := ok && t.Status == TaskRunning
		h.mu.Unlock()
		if running {
			return h
		}
		if ok && ended == nil {
			ended = h
		}
	}
	return ended
}

// hubWithJob finds the listed hub watching the running managed job id.
func hubWithJob(id string) *taskHub {
	hubsMu.Lock()
	defer hubsMu.Unlock()
	for _, h := range hubs {
		found := false
		h.mu.Lock()
		for _, job := range h.backgroundShells {
			if job.ID == id && !job.IsDone() {
				found = true
				break
			}
		}
		h.mu.Unlock()
		if found {
			return h
		}
	}
	return nil
}

// markInterruptedDetached records the process tree at pid, in the hub whose
// agents started it, as killed by InterruptJob, so that a detached process
// among it is not reported as ended.
func markInterruptedDetached(pid int) {
	hubsMu.Lock()
	listed := slices.Clone(hubs)
	hubsMu.Unlock()
	for _, h := range listed {
		procs := markedProcs([]string{TasksDirEnv + "=" + h.dir})
		if _, ok := procs[pid]; !ok {
			continue
		}
		tree := []int{pid}
		for i := 0; i < len(tree); i++ {
			for _, p := range procs {
				if p.ppid == tree[i] {
					tree = append(tree, p.pid)
				}
			}
		}
		h.mu.Lock()
		if h.interruptedDetached == nil {
			h.interruptedDetached = make(map[detachedKey]bool)
		}
		for _, p := range tree {
			h.interruptedDetached[detachedKey{p, procs[p].started}] = true
		}
		h.mu.Unlock()
		return
	}
}
