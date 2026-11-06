package client

// This file held the client's calls before they moved into per-area files
// around the typed rpc descriptors. It now keeps the calls upstream v0.100.0
// added: credential prompts (pinentry and SSH), permission modes, and the
// host application's theme and context.

import (
	"context"
	"net/url"

	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/rpc"
)

// AnswerPinentry submits a secret for the pending integrated pinentry
// prompt on a workspace. Returns true if this call resolved the
// pending prompt, false if already resolved by another caller.
func (c *Client) AnswerPinentry(ctx context.Context, id string, req proto.PinentryAnswer) (bool, error) {
	resp, err := newCall(c, rpc.AnswerPinentry, "answer pinentry prompt", "answer pinentry prompt response").at(id).do(ctx, req)
	return resp.Resolved, err
}

// CancelPinentry dismisses the pending integrated pinentry prompt on
// a workspace. Returns true if a prompt was cancelled, false if none
// was pending.
func (c *Client) CancelPinentry(ctx context.Context, id string, req proto.PinentryCancel) (bool, error) {
	resp, err := newCall(c, rpc.CancelPinentry, "cancel pinentry prompt", "cancel pinentry prompt response").at(id).do(ctx, req)
	return resp.Resolved, err
}

// AnswerSSH submits a secret for the pending integrated SSH prompt on a
// workspace. Returns true if this call resolved the pending prompt,
// false if already resolved by another caller.
func (c *Client) AnswerSSH(ctx context.Context, id string, req proto.SSHAnswer) (bool, error) {
	resp, err := newCall(c, rpc.AnswerSSH, "answer ssh prompt", "answer ssh prompt response").at(id).do(ctx, req)
	return resp.Resolved, err
}

// CancelSSH dismisses the pending integrated SSH prompt on a workspace.
// Returns true if a prompt was cancelled, false if none was pending.
func (c *Client) CancelSSH(ctx context.Context, id string, req proto.SSHCancel) (bool, error) {
	resp, err := newCall(c, rpc.CancelSSH, "cancel ssh prompt", "cancel ssh prompt response").at(id).do(ctx, req)
	return resp.Resolved, err
}

// SetPermissionMode sets the permission mode for a workspace.
func (c *Client) SetPermissionMode(ctx context.Context, id string, mode proto.WorkspacePermissionMode) error {
	_, err := newCall(c, rpc.SetPermissionMode, "set permission mode", "").at(id).
		do(ctx, proto.PermissionSetModeRequest{Mode: mode})
	return err
}

// SetHostTheme sets the theme a host application supplies for every
// client of the workspace, or clears it when theme is nil, and returns the
// resulting state. It fails with an error carrying the server's message
// when a color or the base theme is invalid.
func (c *Client) SetHostTheme(ctx context.Context, id string, theme *proto.HostTheme) (*proto.HostThemeState, error) {
	q := url.Values{"client_id": []string{c.clientID}}
	st, err := newCall(c, rpc.SetHostTheme, "set host theme", "host theme").at(id).with(q).
		do(ctx, proto.HostThemeRequest{Theme: theme})
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// GetHostTheme retrieves the host theme of a workspace. The returned
// state's Theme is nil when none is set.
func (c *Client) GetHostTheme(ctx context.Context, id string) (*proto.HostThemeState, error) {
	st, err := newCall(c, rpc.GetHostTheme, "get host theme", "host theme").at(id).get(ctx)
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// GetHostContext retrieves what the host application has in view for a
// workspace.
func (c *Client) GetHostContext(ctx context.Context, id string) (proto.HostContext, error) {
	return newCall(c, rpc.GetHostContext, "get host context", "host context").at(id).get(ctx)
}
