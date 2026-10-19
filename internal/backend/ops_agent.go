package backend

import (
	"context"
	"log/slog"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/shell"
)

// IsSessionBusy reports whether an agent run is in flight for the
// session.
func (o Ops) IsSessionBusy(sessionID string) bool {
	if o.App.AgentCoordinator == nil {
		return false
	}
	return o.App.AgentCoordinator.IsSessionBusy(sessionID)
}

// CancelSession cancels the session's active run, if any.
func (o Ops) CancelSession(sessionID string) {
	if o.App.AgentCoordinator != nil {
		o.App.AgentCoordinator.Cancel(sessionID)
	}
}

// Interrupt stops the session's active run and lets the next queued
// prompt run.
func (o Ops) Interrupt(sessionID string) {
	if o.App.AgentCoordinator != nil {
		o.App.AgentCoordinator.Interrupt(sessionID)
	}
}

// QueuedPrompts returns the number of prompts queued for the session.
func (o Ops) QueuedPrompts(sessionID string) int {
	if o.App.AgentCoordinator == nil {
		return 0
	}
	return o.App.AgentCoordinator.QueuedPrompts(sessionID)
}

// QueuedPromptsList returns the text of the prompts queued for the
// session.
func (o Ops) QueuedPromptsList(sessionID string) []string {
	if o.App.AgentCoordinator == nil {
		return nil
	}
	return o.App.AgentCoordinator.QueuedPromptsList(sessionID)
}

// QueuedPromptSummaries describes the prompts queued for the session.
func (o Ops) QueuedPromptSummaries(sessionID string) []message.QueuedPromptSummary {
	return agent.QueueSummaries(o.App.AgentCoordinator, sessionID)
}

// ClearQueue drops every prompt queued for the session.
func (o Ops) ClearQueue(sessionID string) {
	if o.App.AgentCoordinator != nil {
		o.App.AgentCoordinator.ClearQueue(sessionID)
	}
}

// RecallQueuedPrompt withdraws the newest queued prompt so the user can
// edit it. It returns nil when nothing is queued.
func (o Ops) RecallQueuedPrompt(sessionID string) (*message.QueuedPrompt, error) {
	if o.App.AgentCoordinator == nil {
		return nil, ErrAgentNotInitialized
	}
	return o.App.AgentCoordinator.RecallQueuedPrompt(sessionID), nil
}

// SetMainAgent switches the active agent (coder or plan). It is
// rejected while the agent is running so a switch can never strand a
// run's queued prompts on the previous agent. A successful switch runs
// the ConfigChanged hook, so every app on the project re-reads its agent
// info; a refused one publishes nothing.
func (o Ops) SetMainAgent(agentID string) error {
	if o.App.AgentCoordinator == nil {
		return ErrAgentNotInitialized
	}
	if o.App.AgentCoordinator.IsBusy() {
		return ErrAgentBusy
	}
	if err := o.App.AgentCoordinator.SetMainAgent(agentID); err != nil {
		return err
	}
	o.configChanged()
	return nil
}

// Summarize summarizes a session.
func (o Ops) Summarize(ctx context.Context, sessionID string) error {
	if o.App.AgentCoordinator == nil {
		return ErrAgentNotInitialized
	}
	return o.App.AgentCoordinator.Summarize(ctx, sessionID)
}

// GetDefaultSmallModel returns the default small model for a provider.
func (o Ops) GetDefaultSmallModel(providerID string) config.SelectedModel {
	return o.App.GetDefaultSmallModel(providerID)
}

// ShellCommand describes a shell command the user typed directly,
// outside of any agent turn.
type ShellCommand struct {
	SessionID string
	Command   string
	// Cwd is the directory the command runs in.
	Cwd string
	// Env is the command's full environment; nil inherits the process
	// environment.
	Env       []string
	TermWidth int
	// OnProgress, when set, receives output as it arrives. The output
	// is then persisted once the command ends, and a failure to run the
	// command is not reported: what was captured is returned instead.
	OnProgress func(string)
	// IsFirstMessage asks for a session title derived from the command.
	IsFirstMessage bool
}

// RunShellCommand runs a user shell command and persists the command and
// its output as a message in the session (when SessionID is set).
func (o Ops) RunShellCommand(ctx context.Context, cmd ShellCommand) (proto.ShellCommandResponse, error) {
	// Oversized output spills into the workspace data directory. Tests
	// build workspaces without a config store; those fall back to the
	// system temp directory.
	var dataDir string
	if o.Cfg != nil {
		dataDir = o.Cfg.Config().Options.DataDirectory
	}

	var persist shell.PersistFunc
	if cmd.SessionID != "" {
		persist = func(command, output string, exitCode int) error {
			return shell.PersistOutput(ctx, o.App.Messages, cmd.SessionID, command, output, exitCode, dataDir)
		}
	}

	opts := shell.RunOptions{
		Command:   cmd.Command,
		Cwd:       cmd.Cwd,
		Env:       cmd.Env,
		TermWidth: cmd.TermWidth,
	}

	var (
		result shell.CaptureResult
		err    error
	)
	if cmd.OnProgress != nil {
		result, err = shell.RunAndCaptureStream(ctx, opts, cmd.OnProgress)
	} else {
		result, err = shell.RunAndPersist(ctx, opts, persist)
	}
	if err != nil && cmd.OnProgress == nil {
		return proto.ShellCommandResponse{}, err
	}

	// The streaming path does not persist on its own.
	if cmd.OnProgress != nil && persist != nil {
		if persistErr := persist(cmd.Command, result.Output, result.ExitCode); persistErr != nil {
			slog.Error("Failed to persist shell command output", "error", persistErr, "command", cmd.Command)
		}
	}

	if cmd.IsFirstMessage && o.App.AgentCoordinator != nil {
		o.App.AgentCoordinator.GenerateTitle(context.WithoutCancel(ctx), cmd.SessionID, "$ "+cmd.Command)
	}

	return proto.ShellCommandResponse{
		Output:   result.Output,
		ExitCode: result.ExitCode,
	}, nil
}
