package backend

import (
	"context"
	"fmt"

	mcptools "github.com/charmbracelet/crush/internal/agent/tools/mcp"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/oauth"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/skills"
)

// publishConfigChanged publishes a ConfigChanged event on the workspace's
// event broker so all subscribers (e.g. remote clients) refresh their
// cached config snapshot. It also re-initializes any MCP servers whose
// configuration changed as a result of the write.
func publishConfigChanged(ws *Workspace) {
	if ws == nil || ws.App == nil {
		return
	}

	// Re-init MCP servers whose config changed. MCP state is process-global,
	// so this only needs to happen once regardless of which workspace
	// triggered the write. Run async so unrelated config writes (model
	// switches, API keys) don't block on MCP reconciliation. Bound to the
	// workspace ctx so teardown cancels any in-flight init.
	if ws.Cfg != nil {
		go mcptools.Reinitialize(ws.ctx, ws.Cfg)
	}

	sendConfigChanged(ws)
}

// sendConfigChanged publishes only the ConfigChanged event, without the
// MCP re-init of [publishConfigChanged]. The MCP toggles use it: they
// apply themselves, and a re-init from the config alone would undo a
// repository-scoped override.
func sendConfigChanged(ws *Workspace) {
	if ws == nil || ws.App == nil {
		return
	}
	ws.SendEvent(pubsub.Event[proto.ConfigChanged]{
		Type:    pubsub.UpdatedEvent,
		Payload: proto.ConfigChanged{WorkspaceID: ws.ID},
	})
}

// MCPResourceContents holds the contents of an MCP resource returned
// by the backend.
type MCPResourceContents = proto.MCPResourceContents

// SetConfigField sets a key/value pair in the config file for the
// given scope.
func (b *Backend) SetConfigField(workspaceID string, scope config.Scope, key string, value any) error {
	return b.SetConfigFieldAs(workspaceID, true, scope, key, value)
}

// SetConfigFieldAs is [Backend.SetConfigField] for a caller that came in
// on the engine's unix socket (local) or not. A caller that is not local
// may not move the engine's CPUs: a write that could change
// options.agent_cpus_enabled or options.agent_cpus in scope's file, by any
// spelling the loader accepts, is refused with [ErrLocalOnly] before
// anything changes. The check is part of the locked write itself.
func (b *Backend) SetConfigFieldAs(workspaceID string, local bool, scope config.Scope, key string, value any) error {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return err
	}
	o := ws.ops()
	if !local {
		return b.writeConfig(ws, func() error {
			return o.mutateConfig(func() error { return ws.Cfg.SetConfigFieldGuarded(scope, key, value, refuseCPUChange) })
		})
	}
	return b.writeConfig(ws, func() error { return o.SetConfigField(scope, key, value) })
}

// refuseCPUChange refuses a config edit that could change the CPU setting.
func refuseCPUChange(before, after []byte) error {
	if config.CPUSettingChanged(before, after) {
		return ErrLocalOnly
	}
	return nil
}

// RemoveConfigField removes a key from the config file for the given
// scope.
func (b *Backend) RemoveConfigField(workspaceID string, scope config.Scope, key string) error {
	return b.RemoveConfigFieldAs(workspaceID, true, scope, key)
}

// RemoveConfigFieldAs is [Backend.RemoveConfigField] for a caller that
// came in on the engine's unix socket (local) or not, with the CPU rule of
// [Backend.SetConfigFieldAs].
func (b *Backend) RemoveConfigFieldAs(workspaceID string, local bool, scope config.Scope, key string) error {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return err
	}
	o := ws.ops()
	if !local {
		return b.writeConfig(ws, func() error {
			return o.mutateConfig(func() error { return ws.Cfg.RemoveConfigFieldGuarded(scope, key, refuseCPUChange) })
		})
	}
	return b.writeConfig(ws, func() error { return o.RemoveConfigField(scope, key) })
}

// UpdatePreferredModel updates the preferred model for the given type
// and persists it to the config file at the given scope.
func (b *Backend) UpdatePreferredModel(workspaceID string, scope config.Scope, modelType config.SelectedModelType, model config.SelectedModel) error {
	o, err := b.ops(workspaceID)
	if err != nil {
		return err
	}
	return o.UpdatePreferredModel(scope, modelType, model)
}

// SetCompactMode sets the compact mode setting and persists it.
func (b *Backend) SetCompactMode(workspaceID string, scope config.Scope, enabled bool) error {
	o, err := b.ops(workspaceID)
	if err != nil {
		return err
	}
	return o.SetCompactMode(scope, enabled)
}

// SetProviderAPIKey sets the API key for a provider and persists it.
// Agents blocked waiting for the provider's credentials are woken.
func (b *Backend) SetProviderAPIKey(workspaceID string, scope config.Scope, providerID string, apiKey any) error {
	o, err := b.ops(workspaceID)
	if err != nil {
		return err
	}
	return o.SetProviderAPIKey(scope, providerID, apiKey)
}

// ImportCopilot attempts to import a GitHub Copilot token from disk.
func (b *Backend) ImportCopilot(workspaceID string) (*oauth.Token, bool, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return nil, false, err
	}
	token, ok := o.ImportCopilot()
	return token, ok, nil
}

// RefreshOAuthToken refreshes the OAuth token for a provider.
func (b *Backend) RefreshOAuthToken(ctx context.Context, workspaceID string, scope config.Scope, providerID string) error {
	o, err := b.ops(workspaceID)
	if err != nil {
		return err
	}
	return o.RefreshOAuthToken(ctx, scope, providerID)
}

// ProjectNeedsInitialization checks whether the project in this
// workspace needs initialization.
func (b *Backend) ProjectNeedsInitialization(workspaceID string) (bool, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return false, err
	}
	return o.ProjectNeedsInitialization()
}

// MarkProjectInitialized marks the project as initialized.
func (b *Backend) MarkProjectInitialized(workspaceID string) error {
	o, err := b.ops(workspaceID)
	if err != nil {
		return err
	}
	return o.MarkProjectInitialized()
}

// InitializePrompt builds the initialization prompt for the workspace.
func (b *Backend) InitializePrompt(workspaceID string) (string, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return "", err
	}
	return o.InitializePrompt()
}

// GitBranch returns the current Git branch of the workspace's working
// directory, or an empty string when it is not a Git repository or HEAD
// is detached.
func (b *Backend) GitBranch(workspaceID string) (string, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return "", err
	}
	return o.GitBranch(), nil
}

// ProviderAvailability says, by provider id, why a provider cannot run in
// the workspace's folder. Empty when every provider can run.
func (b *Backend) ProviderAvailability(workspaceID string) (map[string]string, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return nil, err
	}
	return o.ProviderAvailability(), nil
}

// ReadSkill reads a skill's content by ID.
func (b *Backend) ReadSkill(_ context.Context, workspaceID, skillID string) ([]byte, proto.SkillReadResult, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return nil, proto.SkillReadResult{}, err
	}
	content, result, err := o.ReadSkill(skillID)
	if err != nil {
		return nil, proto.SkillReadResult{}, err
	}
	return content, proto.SkillReadResultFromDomain(result), nil
}

// ListSkills returns the effective visible skills for a workspace.
func (b *Backend) ListSkills(workspaceID string) ([]proto.SkillInfo, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return nil, err
	}
	return proto.SkillInfosFromDomain(o.ListSkills()), nil
}

// InstalledSkills lists the installed skills of a workspace, including
// disabled ones.
func (b *Backend) InstalledSkills(workspaceID string) ([]skills.InstalledSkill, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return nil, err
	}
	return o.InstalledSkills(), nil
}

// ManageSkill installs a skill or toggles whether one is enabled.
func (b *Backend) ManageSkill(ctx context.Context, workspaceID string, req proto.ManageSkillRequest) error {
	o, err := b.ops(workspaceID)
	if err != nil {
		return err
	}
	switch req.Action {
	case proto.ManageSkillToggle:
		return o.SetSkillEnabled(req.Name, req.Enabled)
	case proto.ManageSkillInstall:
		return o.InstallSkill(ctx, req.Skill)
	default:
		return fmt.Errorf("unknown skill action %q", req.Action)
	}
}
