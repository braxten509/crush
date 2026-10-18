package workspace

import (
	"context"

	"github.com/charmbracelet/crush/internal/proto"
)

// Phone sharing and secure entry through the engine (engine-design.md 5.7
// and 5.8). The terminal UI moves to these in phase 7 (5.13); until then it
// shares and enters secrets in-process only.

// StartRemote starts this window's phone share, or returns the running
// one's status. launchID is the window's CRUSH_REMOTE_LAUNCH value, if any.
func (w *ClientWorkspace) StartRemote(ctx context.Context, launchID string) (proto.RemoteStatus, error) {
	return w.client.StartRemote(ctx, w.workspaceID(), launchID)
}

// StopRemote stops this window's phone share.
func (w *ClientWorkspace) StopRemote(ctx context.Context) error {
	return w.client.StopRemote(ctx, w.workspaceID())
}

// RemoteStatus describes this window's phone share.
func (w *ClientWorkspace) RemoteStatus(ctx context.Context) (proto.RemoteStatus, error) {
	return w.client.RemoteStatus(ctx, w.workspaceID())
}

// SetRemotePresence tells this window's phone share what the window shows.
func (w *ClientWorkspace) SetRemotePresence(ctx context.Context, p proto.RemotePresence) error {
	return w.client.SetRemotePresence(ctx, w.workspaceID(), p)
}

// FinishRemoteAction reports the outcome of a phone action the engine
// forwarded to this window.
func (w *ClientWorkspace) FinishRemoteAction(ctx context.Context, actionID string, done proto.RemoteActionDone) error {
	return w.client.FinishRemoteAction(ctx, w.workspaceID(), actionID, done)
}

// SecureForms lists the open secure forms sent to this window.
func (w *ClientWorkspace) SecureForms(ctx context.Context) ([]proto.SecureForm, error) {
	return w.client.SecureForms(ctx, w.workspaceID())
}

// SubmitSecureForm answers a secure form.
func (w *ClientWorkspace) SubmitSecureForm(ctx context.Context, formID string, submit proto.SecureSubmit) error {
	return w.client.SubmitSecureForm(ctx, w.workspaceID(), formID, submit)
}

// CancelSecureForm closes a secure form without saving anything more.
func (w *ClientWorkspace) CancelSecureForm(ctx context.Context, formID string) error {
	return w.client.CancelSecureForm(ctx, w.workspaceID(), formID)
}
