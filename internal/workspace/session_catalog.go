package workspace

import (
	"context"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/sessioncatalog"
)

func (w *AppWorkspace) ListSavedSessions(ctx context.Context, selected string) ([]session.Session, error) {
	return (sessioncatalog.Catalog{Directory: w.WorkingDir(), DataDirectory: w.Config().Options.DataDirectory}).List(ctx, selected)
}
func (w *AppWorkspace) ChangeSavedSession(ctx context.Context, target session.Session, remove bool) error {
	if target.DataDirectory == w.Config().Options.DataDirectory {
		if remove {
			return w.DeleteSession(ctx, target.ID)
		}
		return w.app.Sessions.Rename(ctx, target.ID, target.Title)
	}
	return (sessioncatalog.Catalog{Directory: w.WorkingDir(), DataDirectory: w.Config().Options.DataDirectory}).Change(ctx, target, remove)
}
func (w *ClientWorkspace) ListSavedSessions(ctx context.Context, selected string) ([]session.Session, error) {
	return w.client.ListSavedSessions(ctx, w.workspaceID(), selected)
}
func (w *ClientWorkspace) ChangeSavedSession(ctx context.Context, target session.Session, remove bool) error {
	return w.client.ChangeSavedSession(ctx, w.workspaceID(), target, remove)
}

func (w *AppWorkspace) DiscardEmptySession(ctx context.Context, id string) error {
	return (sessioncatalog.Catalog{Directory: w.WorkingDir(), DataDirectory: w.Config().Options.DataDirectory}).DiscardEmpty(ctx, id)
}
func (w *ClientWorkspace) DiscardEmptySession(ctx context.Context, id string) error {
	return w.client.DiscardEmptySession(ctx, w.workspaceID(), id)
}
