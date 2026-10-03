package client

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/skills"
	"net/http"
)

func (c *Client) InstalledSkills(ctx context.Context, id string) ([]skills.InstalledSkill, error) {
	rsp, err := c.get(ctx, fmt.Sprintf("/workspaces/%s/skills/installed", id), nil, nil)
	if err != nil {
		return nil, err
	}
	defer rsp.Body.Close()
	if rsp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list installed skills: HTTP %d", rsp.StatusCode)
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
	if rsp.StatusCode != http.StatusOK {
		var result struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(rsp.Body).Decode(&result)
		return fmt.Errorf("manage skill: HTTP %d %s", rsp.StatusCode, result.Message)
	}
	return nil
}
