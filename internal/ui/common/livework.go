package common

import (
	"context"
	"log/slog"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/workspace"
)

// Shared means this window runs its chats on the project's engine
// (engine-design.md 5.13), so the agent globals in this process are empty:
// helpers, jobs and limits are read through the workspace instead. A window
// that works alone keeps reading the globals, exactly as before.
func (c *Common) Shared() bool {
	_, ok := c.Workspace.(*workspace.ClientWorkspace)
	return ok
}

func (c *Common) liveWork() workspace.LiveWork {
	if !c.Shared() {
		return nil
	}
	lw, _ := c.Workspace.(workspace.LiveWork)
	return lw
}

// BackgroundJobs lists the background processes the window's row shows:
// every one in this process when working alone (as before), the shown
// chat's on the engine when shared.
func (c *Common) BackgroundJobs(ctx context.Context, sessionID string) []agent.Process {
	lw := c.liveWork()
	if lw == nil {
		return agent.BackgroundProcesses()
	}
	if sessionID == "" {
		return nil
	}
	procs, err := lw.Jobs(ctx, sessionID)
	if err != nil {
		slog.Debug("Failed to list the chat's jobs", "session_id", sessionID, "error", err)
		return nil
	}
	return procs
}

// KillJob stops a background process.
func (c *Common) KillJob(ctx context.Context, p agent.Process) error {
	if lw := c.liveWork(); lw != nil {
		return lw.KillJob(ctx, p.Session, p)
	}
	return agent.StopBackgroundProcess(p)
}

// StopHelper stops a sub-agent.
func (c *Common) StopHelper(ctx context.Context, t agent.Task) error {
	if lw := c.liveWork(); lw != nil {
		return lw.StopHelper(ctx, t.SessionID, t.ID)
	}
	return agent.StopTask(t.ID)
}

// RefreshCLILimits brings the AI tools' usage limits up to date in this
// process's limits cache, which every limits view reads: fetched here when
// working alone (force skips the refresh interval), copied from the
// engine's reading when shared. It reports whether the cache changed.
func (c *Common) RefreshCLILimits(ctx context.Context, kind catwalk.Type, force bool) (bool, error) {
	lw := c.liveWork()
	if lw == nil {
		if force {
			return true, cliagent.RefreshLimitsNow(ctx, kind)
		}
		return cliagent.RefreshLimits(ctx, kind), nil
	}
	all, err := lw.CLILimits(ctx)
	if err != nil {
		return false, err
	}
	changed := false
	for _, l := range all {
		if l.Error != "" && catwalk.Type(l.Kind) == kind && l.Account == "" && len(l.Windows) == 0 {
			err = errorText(l.Error)
		}
		limits := make([]cliagent.Limit, 0, len(l.Windows))
		for _, w := range l.Windows {
			limits = append(limits, cliagent.Limit{Name: w.Name, Used: w.Used, ResetsAt: w.ResetsAt, Model: w.Model})
		}
		if cliagent.StoreAccountLimits(catwalk.Type(l.Kind), l.Account, limits, l.Refills) {
			changed = true
		}
	}
	return changed, err
}

type errorText string

func (e errorText) Error() string { return string(e) }
