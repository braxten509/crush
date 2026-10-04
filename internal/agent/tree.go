package agent

import (
	"context"
	"fmt"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/shell"
)

// SwitchTree is an optional local-workspace capability. Keep the dispatch lock
// through the switch, so a submitted/accepted prompt cannot cross the boundary.
func (c *coordinator) SwitchTree(ctx context.Context, sessionID, target string) error {
	a, ok := c.currentAgent().(*sessionAgent)
	if !ok {
		return fmt.Errorf("tree switching is unavailable for this agent")
	}
	mu := a.sessionMu(sessionID)
	mu.Lock()
	defer mu.Unlock()
	accepted, _ := a.acceptedRuns.Get(sessionID)
	if accepted > 0 || a.IsSessionBusy(sessionID) || a.QueuedPrompts(sessionID) > 0 {
		return fmt.Errorf("wait for the agent and queued prompts before switching branches")
	}
	// Completed tasks can still be delivering a parent notification. Until task
	// delivery has a branch token, keep sessions with task records read-only here.
	if len(SessionTasks(sessionID)) > 0 {
		return fmt.Errorf("tree switching in sessions with background sub-agents is not supported yet")
	}
	jobs := shell.GetBackgroundShellManager()
	for _, id := range jobs.List() {
		if job, ok := jobs.Get(id); ok && !job.IsDone() {
			return fmt.Errorf("wait for background commands before switching branches")
		}
	}
	if err := cliagent.CloseForTree(sessionID); err != nil {
		return err
	}
	tree, ok := c.messages.(message.TreeService)
	if !ok {
		return fmt.Errorf("tree storage unavailable")
	}
	return tree.SwitchTree(ctx, sessionID, target)
}

func (a *sessionAgent) treeCLILink(ctx context.Context, m *cliagent.Model, sessionID string) (cliagent.Link, error) {
	link := m.Links.Get(sessionID, m.Kind)
	if tree, ok := a.messages.(message.TreeService); ok {
		revision, err := tree.TreeRevision(ctx, sessionID)
		if err != nil {
			return cliagent.Link{}, err
		}
		if link.TreeRevision != revision {
			return cliagent.Link{}, nil
		}
	}
	return link, nil
}
