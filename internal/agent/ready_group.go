package agent

import "sync"

// readyGroup tracks the coordinator's one-time setup work (system prompt
// and tool list builds). It replaces an errgroup.Group, whose WaitGroup
// panics with "WaitGroup is reused before previous Wait has returned"
// when Go is called while other goroutines are still returning from
// Wait. That happened here: concurrent turns wait for readiness while
// UpdateModels -> buildTools -> agentTool -> buildAgent registers new
// setup work for the sub-agent.
//
// A failed task stays recorded, so its error keeps being returned like
// errgroup's sticky error.
type readyGroup struct {
	mu    sync.Mutex
	tasks []*readyTask
}

type readyTask struct {
	done chan struct{}
	err  error
}

// Go runs f in a new goroutine and registers it with the group.
func (g *readyGroup) Go(f func() error) {
	task := &readyTask{done: make(chan struct{})}
	g.mu.Lock()
	g.tasks = append(g.tasks, task)
	g.mu.Unlock()
	go func() {
		defer close(task.done)
		task.err = f()
	}()
}

// Wait blocks until every registered task has finished, including tasks
// registered by other tasks while Wait is blocked (a tool build registers
// the sub-agent's setup), and returns the first non-nil error among them.
func (g *readyGroup) Wait() error {
	for {
		g.mu.Lock()
		var pending *readyTask
		for _, task := range g.tasks {
			if !task.finished() {
				pending = task
				break
			}
		}
		if pending == nil {
			err := g.pruneLocked()
			g.mu.Unlock()
			return err
		}
		g.mu.Unlock()
		<-pending.done
	}
}

// pruneLocked drops finished successful tasks so the list stays small
// across repeated model updates and returns the first recorded error;
// failed tasks stay to keep that error sticky. All tasks must be finished.
func (g *readyGroup) pruneLocked() error {
	var firstErr error
	kept := g.tasks[:0:0]
	for _, task := range g.tasks {
		if task.err != nil {
			if firstErr == nil {
				firstErr = task.err
			}
			kept = append(kept, task)
		}
	}
	g.tasks = kept
	return firstErr
}

func (t *readyTask) finished() bool {
	select {
	case <-t.done:
		return true
	default:
		return false
	}
}
