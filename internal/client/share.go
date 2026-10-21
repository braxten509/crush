package client

import (
	"context"
	"net/url"

	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/rpc"
)

// Phone sharing and secure entry through the engine (engine-design.md 5.7
// and 5.8). Every call names this client as the calling app.

// StartRemote starts the phone share of sessionID, the chat this app shows
// ("" for its new chat), or returns the chat's running share's status.
// launchID is the window's CRUSH_REMOTE_LAUNCH value, if any. A setup
// problem or a failed start comes back in the status, not as an error.
func (c *Client) StartRemote(ctx context.Context, id, sessionID, launchID string) (proto.RemoteStatus, error) {
	return newCall(c, rpc.RemoteStart, "start Remote Control", "Remote Control status").at(id).
		with(c.shareAppQuery()).do(ctx, proto.RemoteStart{SessionID: sessionID, LaunchID: launchID})
}

// StopRemote stops the phone share of sessionID ("" for this app's new
// chat), whichever app started it.
func (c *Client) StopRemote(ctx context.Context, id, sessionID string) error {
	_, err := newCall(c, rpc.RemoteStop, "stop Remote Control", "").at(id).
		with(c.shareAppQuery()).do(ctx, proto.RemoteStop{SessionID: sessionID})
	return err
}

// RemoteShares describes the phone shares this app sees: every chat's, and
// its own new chat's.
func (c *Client) RemoteShares(ctx context.Context, id string) ([]proto.RemoteStatus, error) {
	return newCall(c, rpc.RemoteGet, "list the phone shares", "phone shares").at(id).
		with(c.shareAppQuery()).get(ctx)
}

// SetRemotePresence tells the engine what this app's window shows.
func (c *Client) SetRemotePresence(ctx context.Context, id string, p proto.RemotePresence) error {
	_, err := newCall(c, rpc.RemotePresence, "set Remote Control presence", "").at(id).
		with(c.shareAppQuery()).do(ctx, p)
	return err
}

// FinishRemoteAction reports the outcome of a phone action the engine
// forwarded to this app.
func (c *Client) FinishRemoteAction(ctx context.Context, id, actionID string, done proto.RemoteActionDone) error {
	_, err := newCall(c, rpc.RemoteActionDone, "finish phone action", "").at(id, actionID).
		with(c.shareAppQuery()).do(ctx, done)
	return err
}

// SecureForms lists the open secure forms sent to this app.
func (c *Client) SecureForms(ctx context.Context, id string) ([]proto.SecureForm, error) {
	return newCall(c, rpc.ListSecureForms, "list secure entries", "secure entries").at(id).
		with(c.shareAppQuery()).get(ctx)
}

// SubmitSecureForm answers a secure form. The values travel only in this
// request's body, which the engine accepts only on its unix socket.
func (c *Client) SubmitSecureForm(ctx context.Context, id, formID string, submit proto.SecureSubmit) error {
	_, err := newCall(c, rpc.SubmitSecureForm, "submit secure entry", "").at(id, formID).
		with(c.shareAppQuery()).do(ctx, submit)
	return err
}

// CancelSecureForm closes a secure form without saving anything more.
func (c *Client) CancelSecureForm(ctx context.Context, id, formID string) error {
	_, err := newCall(c, rpc.CancelSecureForm, "cancel secure entry", "").at(id, formID).
		with(c.shareAppQuery()).get(ctx)
	return err
}

// shareAppQuery names this client as the calling app.
func (c *Client) shareAppQuery() url.Values {
	return url.Values{"client_id": []string{c.clientID}}
}
