package workspace

import (
	"context"

	"github.com/charmbracelet/crush/internal/session"
)

func (w *AppWorkspace) ListSavedSessions(ctx context.Context, selected string) ([]session.Session, error) {
	return w.ops.ListSavedSessions(ctx, selected)
}

func (w *AppWorkspace) ChangeSavedSession(ctx context.Context, target session.Session, remove bool) error {
	return w.ops.ChangeSavedSession(ctx, target, remove)
}

func (w *AppWorkspace) DiscardEmptySession(ctx context.Context, id string) error {
	return w.ops.DiscardEmptySession(ctx, id)
}

func (w *ClientWorkspace) ListSavedSessions(ctx context.Context, selected string) ([]session.Session, error) {
	return w.client.ListSavedSessions(ctx, w.workspaceID(), selected)
}

func (w *ClientWorkspace) ChangeSavedSession(ctx context.Context, target session.Session, remove bool) error {
	return w.client.ChangeSavedSession(ctx, w.workspaceID(), target, remove)
}

func (w *ClientWorkspace) DiscardEmptySession(ctx context.Context, id string) error {
	return w.client.DiscardEmptySession(ctx, w.workspaceID(), id)
}
