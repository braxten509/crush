package client

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/charmbracelet/crush/internal/session"
	"net/http"
	"net/url"
)

func (c *Client) ListSavedSessions(ctx context.Context, workspace, selected string) ([]session.Session, error) {
	response, err := c.get(ctx, fmt.Sprintf("/workspaces/%s/saved-sessions", workspace), url.Values{"selected": []string{selected}}, nil)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cannot list saved chats: status %d", response.StatusCode)
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
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("cannot change saved chat: status %d", response.StatusCode)
	}
	return nil
}

func (c *Client) DiscardEmptySession(ctx context.Context, workspace, id string) error {
	response, err := c.post(ctx, fmt.Sprintf("/workspaces/%s/saved-sessions", workspace), nil, jsonBody(session.CatalogChange{Session: session.Session{ID: id}, DiscardEmpty: true}), http.Header{"Content-Type": []string{"application/json"}})
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("cannot discard empty chat: status %d", response.StatusCode)
	}
	return nil
}
