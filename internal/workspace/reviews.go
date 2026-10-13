package workspace

import (
	"context"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/proto"
)

func (w *AppWorkspace) LoadMessageReview(ctx context.Context, sessionID, messageID string) (message.Message, error) {
	return w.ops.LoadMessageReview(ctx, sessionID, messageID)
}

func (w *ClientWorkspace) LoadMessageReview(ctx context.Context, sessionID, messageID string) (message.Message, error) {
	msg, err := w.client.LoadMessageReview(ctx, w.workspaceID(), sessionID, messageID)
	if err != nil {
		return message.Message{}, err
	}
	return proto.MessageToDomain(msg), nil
}
