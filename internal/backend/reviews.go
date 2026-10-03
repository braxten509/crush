package backend

import (
	"context"
	"database/sql"

	"github.com/charmbracelet/crush/internal/message"
)

func (b *Backend) LoadMessageReview(ctx context.Context, workspaceID, sessionID, messageID string) (message.Message, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return message.Message{}, err
	}
	msg, err := ws.Messages.Get(ctx, messageID)
	if err != nil {
		return message.Message{}, err
	}
	if msg.SessionID != sessionID {
		return message.Message{}, sql.ErrNoRows
	}
	return ws.Messages.LoadReview(ctx, messageID)
}
