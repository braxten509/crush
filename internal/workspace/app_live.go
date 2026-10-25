package workspace

import (
	"context"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/backend"
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

func (w *AppWorkspace) CLILimits(context.Context) ([]proto.CLILimits, error) {
	return backend.CurrentLimits().CLIs, nil
}
