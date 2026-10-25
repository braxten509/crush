package workspace

import (
	"context"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/proto"
)

// LiveWork lists and stops a chat's helpers and background jobs, and reads
// what the AI tools can do and how much of their usage is left
// (engine-design.md 5.3, 5.4 and 5.9). It is optional, like
// TreeManagement: the in-process workspace calls the agent globals, the
// client workspace the engine's routes.
type LiveWork interface {
	Helpers(ctx context.Context, sessionID string) ([]agent.Task, error)
	StopHelper(ctx context.Context, sessionID, id string) error
	Jobs(ctx context.Context, sessionID string) ([]agent.Process, error)
	KillJob(ctx context.Context, sessionID string, p agent.Process) error
	CLILimits(ctx context.Context) ([]proto.CLILimits, error)
}

var (
	_ LiveWork = (*AppWorkspace)(nil)
	_ LiveWork = (*ClientWorkspace)(nil)
)

// taskFromHelper turns a helper from the engine back into the agent's task.
func taskFromHelper(h proto.Helper) agent.Task {
	return agent.Task{
		ID:        h.ID,
		SessionID: h.SessionID,
		ChildID:   h.ChildSessionID,
		Name:      h.Name,
		CLI:       h.CLI,
		Model:     h.Model,
		Effort:    h.Effort,
		Fast:      h.Fast,
		ReadOnly:  h.ReadOnly,
		Status:    agent.TaskStatus(h.Status),
		Started:   h.Started,
		Ended:     h.Ended,
		Delivered: h.Delivered,
		StartedBy: h.StartedBy,
	}
}

// processFromJob turns a job from the engine back into the agent's process.
func processFromJob(j proto.Job) agent.Process {
	return agent.Process{
		PID:     j.PID,
		JobID:   j.JobID,
		Command: j.Command,
		Name:    j.Name,
		Started: j.Started,
		Session: j.SessionID,
	}
}
