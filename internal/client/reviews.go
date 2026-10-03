package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/charmbracelet/crush/internal/proto"
)

func (c *Client) LoadMessageReview(ctx context.Context, workspaceID, sessionID, messageID string) (proto.Message, error) {
	rsp, err := c.get(ctx, fmt.Sprintf("/workspaces/%s/sessions/%s/messages/%s/review", workspaceID, sessionID, messageID), nil, nil)
	if err != nil {
		return proto.Message{}, err
	}
	defer rsp.Body.Close()
	if rsp.StatusCode != http.StatusOK {
		return proto.Message{}, fmt.Errorf("diff details are unavailable (only the latest five replies are retained)")
	}
	var msg proto.Message
	err = json.NewDecoder(rsp.Body).Decode(&msg)
	return msg, err
}
