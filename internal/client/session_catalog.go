package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/charmbracelet/crush/internal/session"
)

func (c *Client) ListSavedSessions(ctx context.Context, workspace, selected string) ([]session.Session, error) {
	response, err := c.get(ctx, fmt.Sprintf("/workspaces/%s/saved-sessions", workspace), url.Values{"selected": []string{selected}}, nil)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if err := checkStatus(response); err != nil {
		return nil, fmt.Errorf("cannot list saved chats: %w", err)
	}
	var result []session.Session
	err = json.NewDecoder(response.Body).Decode(&result)
	return result, err
}

func (c *Client) ChangeSavedSession(ctx context.Context, workspace string, target session.Session, remove bool) error {
	response, err := c.post(ctx, fmt.Sprintf("/workspaces/%s/saved-sessions", workspace), nil, jsonBody(session.CatalogChange{Session: target, Remove: remove}), http.Header{"Content-Type": []string{"application/json"}})
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if err := checkStatus(response); err != nil {
		return fmt.Errorf("cannot change saved chat: %w", err)
	}
	return nil
}

func (c *Client) DiscardEmptySession(ctx context.Context, workspace, id string) error {
	response, err := c.post(ctx, fmt.Sprintf("/workspaces/%s/saved-sessions", workspace), nil, jsonBody(session.CatalogChange{Session: session.Session{ID: id}, DiscardEmpty: true}), http.Header{"Content-Type": []string{"application/json"}})
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if err := checkStatus(response); err != nil {
		return fmt.Errorf("cannot discard empty chat: %w", err)
	}
	return nil
}
