package workspace

import (
	"context"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/proto"
)

// LiveWork in process: the agent globals, scoped to the chat as the engine
// scopes them.

func (w *AppWorkspace) Helpers(_ context.Context, sessionID string) ([]agent.Task, error) {
	return agent.SessionTasks(sessionID), nil
}

func (w *AppWorkspace) StopHelper(_ context.Context, sessionID, id string) error {
	return agent.StopSessionTask(sessionID, id)
}

func (w *AppWorkspace) Jobs(_ context.Context, sessionID string) ([]agent.Process, error) {
	return agent.SessionBackgroundProcesses(sessionID), nil
}

func (w *AppWorkspace) KillJob(_ context.Context, sessionID string, p agent.Process) error {
	for _, listed := range agent.SessionBackgroundProcesses(sessionID) {
		if p.JobID != "" && listed.JobID == p.JobID || p.JobID == "" && p.PID != 0 && listed.PID == p.PID {
			return agent.StopBackgroundProcess(listed)
		}
	}
	return backend.ErrJobNotFound
}

// CanBackground reads the current coder model the way the terminal's
// Ctrl+B hint does.
func (w *AppWorkspace) CanBackground(context.Context) (bool, error) {
	if w.app.AgentCoordinator == nil {
		return false, nil
	}
	provider := w.app.AgentCoordinator.Model().ModelCfg.Provider
	return cliagent.CanBackground(catwalk.Type(provider)) || provider == config.AbacusProviderID, nil
}

func (w *AppWorkspace) CLILimits(context.Context) ([]proto.CLILimits, error) {
	return backend.CurrentLimits().CLIs, nil
}
