package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/charmbracelet/crush/internal/message"
)

func (c *Client) GetAgentSessionQueuedPromptSummaries(ctx context.Context, id, sessionID string) ([]message.QueuedPromptSummary, error) {
	rsp, err := c.get(ctx, fmt.Sprintf("/workspaces/%s/agent/sessions/%s/prompts/list", id, sessionID), url.Values{"details": {"true"}}, nil)
	if err != nil {
		return nil, err
	}
	defer rsp.Body.Close()
	if err := checkStatus(rsp); err != nil {
		return nil, fmt.Errorf("queued prompts: %w", err)
	}
	var result []message.QueuedPromptSummary
	err = json.NewDecoder(rsp.Body).Decode(&result)
	return result, err
}
