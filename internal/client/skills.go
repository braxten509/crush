package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/skills"
)

func (c *Client) InstalledSkills(ctx context.Context, id string) ([]skills.InstalledSkill, error) {
	rsp, err := c.get(ctx, fmt.Sprintf("/workspaces/%s/skills/installed", id), nil, nil)
	if err != nil {
		return nil, err
	}
	defer rsp.Body.Close()
	if err := checkStatus(rsp); err != nil {
		return nil, fmt.Errorf("list installed skills: %w", err)
	}
	var entries []skills.InstalledSkill
	err = json.NewDecoder(rsp.Body).Decode(&entries)
	return entries, err
}

func (c *Client) ManageSkill(ctx context.Context, id, action, name string, enabled bool, selected skills.DirectorySkill) error {
	rsp, err := c.post(ctx, fmt.Sprintf("/workspaces/%s/skills/manage", id), nil, jsonBody(proto.ManageSkillRequest{Action: action, Name: name, Enabled: enabled, Skill: selected}), http.Header{"Content-Type": {"application/json"}})
	if err != nil {
		return err
	}
	defer rsp.Body.Close()
	if err := checkStatus(rsp); err != nil {
		return fmt.Errorf("manage skill: %w", err)
	}
	return nil
}
