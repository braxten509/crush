package client

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/charmbracelet/crush/internal/message"
	"net/http"
	"net/url"
)

func (c *Client) GetAgentSessionQueuedPromptSummaries(ctx context.Context, id, sessionID string) ([]message.QueuedPromptSummary, error) {
	rsp, err := c.get(ctx, fmt.Sprintf("/workspaces/%s/agent/sessions/%s/prompts/list", id, sessionID), url.Values{"details": {"true"}}, nil)
	if err != nil {
		return nil, err
	}
	defer rsp.Body.Close()
	if rsp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("queued prompts: HTTP %d", rsp.StatusCode)
	}
	var result []message.QueuedPromptSummary
	err = json.NewDecoder(rsp.Body).Decode(&result)
	return result, err
}
