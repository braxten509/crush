package agent

import (
	"context"

	"github.com/charmbracelet/crush/internal/message"
)

// Existing capture assertions exercise the details the user opens. Normal
// history intentionally contains only summaries now.
func loadFileReviewMessages(service message.Service, ctx context.Context, sessionID string) ([]message.Message, error) {
	messages, err := service.List(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	for i, msg := range messages {
		for _, result := range msg.ToolResults() {
			if result.Review != nil && result.Review.Summary != nil {
				messages[i], err = service.LoadReview(ctx, msg.ID)
				if err != nil {
					return nil, err
				}
				break
			}
		}
	}
	return messages, nil
}
