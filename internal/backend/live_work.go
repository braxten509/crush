package backend

import (
	"errors"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/proto"
)

// Helpers and background jobs, scoped to one chat (engine-design.md 5.3 and
// 5.4).

// ListHelpers lists the chat's helpers, running or ended. A helper's own
// helpers are listed under its child chat.
func (b *Backend) ListHelpers(workspaceID, sessionID string) ([]proto.Helper, error) {
	if _, err := b.GetWorkspace(workspaceID); err != nil {
		return nil, err
	}
	tasks := agent.SessionTasks(sessionID)
	out := make([]proto.Helper, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, proto.HelperFromTask(t))
	}
	return out, nil
}

// StopHelper stops one of the chat's helpers the way the user's stop does:
// its parent is told. A helper of another chat is ErrHelperNotFound, and an
// ended one is left as it is.
func (b *Backend) StopHelper(workspaceID, sessionID, helperID string) error {
	if _, err := b.GetWorkspace(workspaceID); err != nil {
		return err
	}
	if err := agent.StopSessionTask(sessionID, helperID); err != nil {
		if errors.Is(err, agent.ErrTaskNotInSession) {
			return ErrHelperNotFound
		}
		return err
	}
	return nil
}

// ListJobs lists the commands running in the background for the chat. It
// reads every chat's jobs, as SessionBackgroundProcesses does, and forgets
// the workspace's jobs that are gone from that list.
func (b *Backend) ListJobs(workspaceID, sessionID string) ([]proto.Job, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	gens := ws.live.jobGens()
	all := agent.BackgroundProcesses()
	ws.live.forgetEndedJobs(gens, all)
	out := make([]proto.Job, 0)
	ws.live.mu.Lock()
	defer ws.live.mu.Unlock()
	for _, p := range all {
		if p.Session != sessionID {
			continue
		}
		out = append(out, proto.Job{
			PID:       p.PID,
			JobID:     p.JobID,
			Command:   p.Command,
			Name:      p.Name,
			Started:   p.Started,
			SessionID: p.Session,
			StartedBy: ws.live.owners[jobKey(p)],
		})
	}
	return out, nil
}

// KillJob ends one of the chat's jobs as the terminal's kill does. A job
// that is not in the chat's list is ErrJobNotFound, and keeps running.
func (b *Backend) KillJob(workspaceID, sessionID string, ref proto.JobRef) error {
	if _, err := b.GetWorkspace(workspaceID); err != nil {
		return err
	}
	for _, p := range agent.SessionBackgroundProcesses(sessionID) {
		if ref.JobID != "" && p.JobID == ref.JobID || ref.JobID == "" && ref.PID != 0 && p.PID == ref.PID {
			return agent.StopBackgroundProcess(p)
		}
	}
	return ErrJobNotFound
}

// MoveToBackground moves the chat's running command to the background, as
// Ctrl+B does in the terminal. It reports whether there was one; false is
// not an error.
func (b *Backend) MoveToBackground(workspaceID, sessionID string) (bool, error) {
	if _, err := b.GetWorkspace(workspaceID); err != nil {
		return false, err
	}
	return cliagent.Background(sessionID) || tools.BackgroundShell(sessionID), nil
}

// AgentCapabilities tells what the current coder model can do, read the way
// the terminal's Ctrl+B hint reads it (ui/model/workspace_cache.go).
func (b *Backend) AgentCapabilities(workspaceID string) (proto.AgentCapabilities, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return proto.AgentCapabilities{}, err
	}
	c := ws.coordinator()
	if c == nil {
		return proto.AgentCapabilities{}, nil
	}
	model := c.Model()
	provider := model.ModelCfg.Provider
	caps := proto.AgentCapabilities{
		Kind:          provider,
		CanBackground: cliagent.CanBackground(catwalk.Type(provider)) || provider == config.AbacusProviderID,
	}
	if cfg := shareConfig(ws); cfg != nil && cfg.Providers != nil {
		if pc, ok := cfg.Providers.Get(provider); ok {
			caps.SupportsFast = config.SupportsFastMode(pc, model.CatwalkCfg)
			caps.SupportsUltracode = config.SupportsUltracode(pc, model.CatwalkCfg)
			caps.SupportsThinkingToggle = config.SupportsThinkingToggle(pc, model.CatwalkCfg)
		}
	}
	return caps, nil
}
