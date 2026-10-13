package client

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/oauth"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/rpc"
	"github.com/charmbracelet/crush/internal/skills"
)

// SetConfigField sets a config key/value pair on the server.
func (c *Client) SetConfigField(ctx context.Context, id string, scope config.Scope, key string, value any) error {
	_, err := newCall(c, rpc.SetConfigField, "set config field", "").at(id).
		do(ctx, proto.ConfigSetRequest{Scope: scope, Key: key, Value: value})
	return err
}

// RemoveConfigField removes a config key on the server.
func (c *Client) RemoveConfigField(ctx context.Context, id string, scope config.Scope, key string) error {
	_, err := newCall(c, rpc.RemoveConfigField, "remove config field", "").at(id).
		do(ctx, proto.ConfigRemoveRequest{Scope: scope, Key: key})
	return err
}

// UpdatePreferredModel updates the preferred model on the server.
func (c *Client) UpdatePreferredModel(ctx context.Context, id string, scope config.Scope, modelType config.SelectedModelType, model config.SelectedModel) error {
	_, err := newCall(c, rpc.UpdatePreferredModel, "update preferred model", "").at(id).
		do(ctx, proto.ConfigModelRequest{Scope: scope, ModelType: modelType, Model: model})
	return err
}

// SetCompactMode sets compact mode on the server.
func (c *Client) SetCompactMode(ctx context.Context, id string, scope config.Scope, enabled bool) error {
	_, err := newCall(c, rpc.SetCompactMode, "set compact mode", "").at(id).
		do(ctx, proto.ConfigCompactRequest{Scope: scope, Enabled: enabled})
	return err
}

// SetProviderAPIKey sets a provider API key on the server. The wire
// format tags the credential with an explicit Kind so the server can
// decode it back into the right Go type — JSON's `any` loses that
// information across the socket.
func (c *Client) SetProviderAPIKey(ctx context.Context, id string, scope config.Scope, providerID string, apiKey any) error {
	var (
		kind proto.APIKeyKind
		raw  json.RawMessage
	)
	switch v := apiKey.(type) {
	case string:
		kind = proto.APIKeyKindString
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("failed to marshal api key string: %w", err)
		}
		raw = b
	case *oauth.Token:
		if v == nil {
			return fmt.Errorf("oauth token is nil")
		}
		kind = proto.APIKeyKindOAuth
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("failed to marshal oauth token: %w", err)
		}
		raw = b
	default:
		return fmt.Errorf("unsupported api key type %T", apiKey)
	}

	_, err := newCall(c, rpc.SetProviderAPIKey, "set provider API key", "").at(id).do(ctx, proto.ConfigProviderKeyRequest{
		Scope:      scope,
		ProviderID: providerID,
		Kind:       kind,
		APIKey:     raw,
	})
	return err
}

// ImportCopilot attempts to import a GitHub Copilot token on the
// server.
func (c *Client) ImportCopilot(ctx context.Context, id string) (*oauth.Token, bool, error) {
	result, err := newCall(c, rpc.ImportCopilot, "import copilot", "import copilot response").at(id).get(ctx)
	if err != nil {
		return nil, false, err
	}
	return result.Token, result.Success, nil
}

// RefreshOAuthToken refreshes an OAuth token for a provider on the
// server.
func (c *Client) RefreshOAuthToken(ctx context.Context, id string, scope config.Scope, providerID string) error {
	_, err := newCall(c, rpc.RefreshOAuthToken, "refresh OAuth token", "").at(id).
		do(ctx, proto.ConfigRefreshOAuthRequest{Scope: scope, ProviderID: providerID})
	return err
}

// ListSkills retrieves the visible skills for a workspace.
func (c *Client) ListSkills(ctx context.Context, id string) ([]proto.SkillInfo, error) {
	return newCall(c, rpc.ListSkills, "list skills", "skills").at(id).get(ctx)
}

// ReadSkill reads a skill's content by ID from the server.
func (c *Client) ReadSkill(ctx context.Context, id, skillID string) (*proto.ReadSkillResponse, error) {
	result, err := newCall(c, rpc.ReadSkill, "read skill", "skill response").at(id).
		do(ctx, proto.ReadSkillRequest{SkillID: skillID})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// InstalledSkills lists the workspace's installed skills, including
// disabled ones.
func (c *Client) InstalledSkills(ctx context.Context, id string) ([]skills.InstalledSkill, error) {
	return newCall(c, rpc.InstalledSkills, "list installed skills", "installed skills").at(id).get(ctx)
}

// ManageSkill installs a skill or toggles whether one is enabled. action
// is [proto.ManageSkillInstall] or [proto.ManageSkillToggle].
func (c *Client) ManageSkill(ctx context.Context, id, action, name string, enabled bool, selected skills.DirectorySkill) error {
	_, err := newCall(c, rpc.ManageSkill, "manage skill", "").at(id).
		do(ctx, proto.ManageSkillRequest{Action: action, Name: name, Enabled: enabled, Skill: selected})
	return err
}
