package client

import (
	"context"
	"net/url"

	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/rpc"
)

// Live work and the smaller gaps (engine-design.md 5.3, 5.4 and 5.9).

// ListHelpers lists a chat's helpers, running or ended.
func (c *Client) ListHelpers(ctx context.Context, workspaceID, sessionID string) ([]proto.Helper, error) {
	return newCall(c, rpc.ListHelpers, "list helpers", "helpers").at(workspaceID, sessionID).get(ctx)
}

// StopHelper stops one of a chat's helpers; its parent chat is told.
func (c *Client) StopHelper(ctx context.Context, workspaceID, sessionID, helperID string) error {
	_, err := newCall(c, rpc.StopHelper, "stop helper", "").at(workspaceID, sessionID, helperID).
		with(url.Values{"client_id": []string{c.clientID}}).get(ctx)
	return err
}

// ListJobs lists the commands running in the background for a chat.
func (c *Client) ListJobs(ctx context.Context, workspaceID, sessionID string) ([]proto.Job, error) {
	return newCall(c, rpc.ListJobs, "list background jobs", "background jobs").at(workspaceID, sessionID).get(ctx)
}

// KillJob ends one of a chat's background jobs.
func (c *Client) KillJob(ctx context.Context, workspaceID, sessionID string, ref proto.JobRef) error {
	_, err := newCall(c, rpc.KillJob, "kill background job", "").at(workspaceID, sessionID).
		with(url.Values{"client_id": []string{c.clientID}}).do(ctx, ref)
	return err
}

// MoveToBackground moves a chat's running command to the background
// (Ctrl+B). It reports whether there was one.
func (c *Client) MoveToBackground(ctx context.Context, workspaceID, sessionID string) (bool, error) {
	res, err := newCall(c, rpc.MoveToBackground, "move command to the background", "background result").at(workspaceID, sessionID).
		with(url.Values{"client_id": []string{c.clientID}}).get(ctx)
	return res.Moved, err
}

// AgentCapabilities tells what chat sessionID's own model can do; "" asks
// for the last pick.
func (c *Client) AgentCapabilities(ctx context.Context, workspaceID, sessionID string) (proto.AgentCapabilities, error) {
	return newCall(c, rpc.AgentCapabilities, "get agent capabilities", "agent capabilities").at(workspaceID).
		with(url.Values{"session": []string{sessionID}}).get(ctx)
}

// Limits returns every AI tool's usage limits as the engine last read them.
func (c *Client) Limits(ctx context.Context) (proto.LimitsSnapshot, error) {
	return newCall(c, rpc.Limits, "get usage limits", "usage limits").get(ctx)
}

// RunShellCommandLive is RunShellCommand with live output: the engine sends
// this client the output as shell_output events carrying requestID.
func (c *Client) RunShellCommandLive(ctx context.Context, id, sessionID, command string, termWidth int, isFirstMessage bool, requestID string) (proto.ShellCommandResponse, error) {
	return newCall(c, rpc.RunShellCommand, "run shell command", "shell command response").at(id, sessionID).
		with(url.Values{"client_id": []string{c.clientID}, "request_id": []string{requestID}}).
		do(ctx, proto.ShellCommandRequest{Command: command, TermWidth: termWidth, IsFirstMessage: isFirstMessage})
}
