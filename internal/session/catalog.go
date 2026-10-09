package session

import (
	"context"
	"path/filepath"
)

// CatalogWorkspace is optional so small workspace adapters can keep a local list.
type CatalogWorkspace interface {
	ListSavedSessions(context.Context, string) ([]Session, error)
	ChangeSavedSession(context.Context, Session, bool) error
}

type EmptySessionWorkspace interface {
	DiscardEmptySession(context.Context, string) error
}

type CatalogChange struct {
	DiscardEmpty bool
	Session      Session
	Remove       bool
}

// CatalogID keeps copied databases' rows distinct in the global picker.
func (s Session) CatalogID() string {
	if s.DataDirectory == "" {
		return s.ID
	}
	return filepath.Clean(s.DataDirectory) + "#" + s.ID
}
