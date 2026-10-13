package client

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/charmbracelet/crush/internal/proto"
)

func (c *Client) LoadMessageReview(ctx context.Context, workspaceID, sessionID, messageID string) (proto.Message, error) {
	rsp, err := c.get(ctx, fmt.Sprintf("/workspaces/%s/sessions/%s/messages/%s/review", workspaceID, sessionID, messageID), nil, nil)
	if err != nil {
		return proto.Message{}, err
	}
	defer rsp.Body.Close()
	if err := checkStatus(rsp); err != nil {
		return proto.Message{}, &reviewUnavailableError{err: err}
	}
	var msg proto.Message
	err = json.NewDecoder(rsp.Body).Decode(&msg)
	return msg, err
}

// reviewUnavailableError keeps the user-facing explanation for a missing
// review while still exposing the underlying status error (for example
// [ErrNotFound]) to errors.Is.
type reviewUnavailableError struct{ err error }

func (e *reviewUnavailableError) Error() string {
	return "diff details are unavailable (only the latest five prompts keep them)"
}

func (e *reviewUnavailableError) Unwrap() error { return e.err }
