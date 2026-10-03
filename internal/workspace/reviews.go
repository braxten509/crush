package workspace

import (
	"context"
	"fmt"

	"github.com/charmbracelet/crush/internal/message"
)

func (w *AppWorkspace) LoadMessageReview(ctx context.Context, sessionID, messageID string) (message.Message, error) {
	msg, err := w.app.Messages.Get(ctx, messageID)
	if err != nil {
		return message.Message{}, err
	}
	if msg.SessionID != sessionID {
		return message.Message{}, fmt.Errorf("review does not belong to this session")
	}
	return w.app.Messages.LoadReview(ctx, messageID)
}

func (w *ClientWorkspace) LoadMessageReview(ctx context.Context, sessionID, messageID string) (message.Message, error) {
	msg, err := w.client.LoadMessageReview(ctx, w.workspaceID(), sessionID, messageID)
	if err != nil {
		return message.Message{}, err
	}
	return protoToMessage(msg), nil
}
