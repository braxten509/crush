package session

import (
	"context"

	"github.com/charmbracelet/crush/internal/pubsub"
)

// Announce publishes a CreatedEvent for a chat written directly to the
// database (copies and branches), so every app sees the new chat
// (engine-design.md 5.6). Callers reach it through an optional interface on
// the service; Service itself is unchanged.
func (s *service) Announce(ctx context.Context, id string) error {
	session, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	s.Publish(pubsub.CreatedEvent, session)
	return nil
}
