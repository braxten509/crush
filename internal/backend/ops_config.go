package backend

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/crush/internal/agent"
	mcptools "github.com/charmbracelet/crush/internal/agent/tools/mcp"
	"github.com/charmbracelet/crush/internal/commands"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/gitutil"
	"github.com/charmbracelet/crush/internal/oauth"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/session"
)

// mutateConfig runs a config write and, when it succeeds, the
// ConfigChanged hook.
func (o Ops) mutateConfig(write func() error) error {
	if err := write(); err != nil {
		return err
	}
	o.configChanged()
	return nil
}

// SetConfigField sets a key/value pair in the config file for scope.
func (o Ops) SetConfigField(scope config.Scope, key string, value any) error {
	return o.mutateConfig(func() error { return o.Cfg.SetConfigField(scope, key, value) })
}

// SetConfigFields sets several key/value pairs in the config file for
// scope.
func (o Ops) SetConfigFields(scope config.Scope, fields map[string]any) error {
	return o.mutateConfig(func() error { return o.Cfg.SetConfigFields(scope, fields) })
}

// RemoveConfigField removes a key from the config file for scope.
func (o Ops) RemoveConfigField(scope config.Scope, key string) error {
	return o.mutateConfig(func() error { return o.Cfg.RemoveConfigField(scope, key) })
}

// ErrChatOwnsModel refuses a write of a folder-wide chat model: each chat
// owns its model, and only the helper model is set in config.
var ErrChatOwnsModel = errors.New("each chat owns its model; pick it in the chat")

// UpdatePreferredModel updates the preferred model for a type and
// persists it.
func (o Ops) UpdatePreferredModel(scope config.Scope, modelType config.SelectedModelType, model config.SelectedModel) error {
	if modelType != config.SelectedModelTypeSmall {
		return ErrChatOwnsModel
	}
	return o.mutateConfig(func() error { return o.Cfg.UpdatePreferredModel(scope, modelType, model) })
}

// SetCompactMode sets and persists the compact mode setting.
func (o Ops) SetCompactMode(scope config.Scope, enabled bool) error {
	return o.mutateConfig(func() error { return o.Cfg.SetCompactMode(scope, enabled) })
}

// SetProviderAPIKey sets and persists a provider's API key, then wakes
// any agent blocked in WaitForTokenChange for that provider.
func (o Ops) SetProviderAPIKey(scope config.Scope, providerID string, apiKey any) error {
	if err := o.Cfg.SetProviderAPIKey(scope, providerID, apiKey); err != nil {
		return err
	}
	o.Cfg.SignalAuthComplete(providerID)
	o.configChanged()
	return nil
}

// ImportCopilot attempts to import a GitHub Copilot token from disk.
func (o Ops) ImportCopilot() (*oauth.Token, bool) {
	token, ok := o.Cfg.ImportCopilot()
	if ok {
		o.configChanged()
	}
	return token, ok
}

// RefreshOAuthToken refreshes a provider's OAuth token.
func (o Ops) RefreshOAuthToken(ctx context.Context, scope config.Scope, providerID string) error {
	return o.mutateConfig(func() error { return o.Cfg.RefreshOAuthToken(ctx, scope, providerID) })
}

// ProjectNeedsInitialization reports whether the project needs
// initialization.
func (o Ops) ProjectNeedsInitialization() (bool, error) {
	return config.ProjectNeedsInitialization(o.Cfg)
}

// MarkProjectInitialized marks the project as initialized.
func (o Ops) MarkProjectInitialized() error {
	return o.mutateConfig(func() error { return config.MarkProjectInitialized(o.Cfg) })
}

// InitializePrompt builds the project initialization prompt.
func (o Ops) InitializePrompt() (string, error) {
	return agent.InitializePrompt(o.Cfg)
}

// GitBranch returns the current Git branch of the working directory, or
// an empty string when it is not a Git repository or HEAD is detached.
func (o Ops) GitBranch() string {
	return gitutil.CurrentBranch(o.Cfg.WorkingDir())
}

// ProviderAvailability says, by provider id, why a provider cannot run in
// the workspace's folder. Empty when every provider can run.
func (o Ops) ProviderAvailability() map[string]string {
	dir := o.Cfg.WorkingDir()
	ids := []string{agent.KimiProviderID}
	for id := range o.Cfg.Config().Providers.Seq2() {
		ids = append(ids, id)
	}
	out := make(map[string]string)
	for _, id := range ids {
		if reason := agent.KimiUnusable(id, dir); reason != "" {
			out[id] = reason
		}
	}
	return out
}

// -- MCP --

// EnableDockerMCP validates Docker MCP availability, stages the
// configuration, starts the MCP client, and persists the config.
func (o Ops) EnableDockerMCP(ctx context.Context) error {
	mcpConfig, err := o.Cfg.PrepareDockerMCPConfig()
	if err != nil {
		return err
	}

	if err := mcptools.InitializeSingle(ctx, config.DockerMCPName, o.Cfg); err != nil {
		disableErr := mcptools.DisableSingle(o.Cfg, config.DockerMCPName)
		o.Cfg.RemoveDockerMCPInMemory()
		return fmt.Errorf("failed to start docker MCP: %w", errors.Join(err, disableErr))
	}

	if err := o.Cfg.PersistDockerMCPConfig(mcpConfig); err != nil {
		disableErr := mcptools.DisableSingle(o.Cfg, config.DockerMCPName)
		o.Cfg.RemoveDockerMCPInMemory()
		return fmt.Errorf("docker MCP started but failed to persist configuration: %w", errors.Join(err, disableErr))
	}

	o.configChanged()
	return nil
}

// DisableDockerMCP closes the Docker MCP client, removes the
// configuration, and persists the change.
func (o Ops) DisableDockerMCP() error {
	if err := mcptools.DisableSingle(o.Cfg, config.DockerMCPName); err != nil {
		return fmt.Errorf("failed to disable docker MCP: %w", err)
	}
	return o.mutateConfig(o.Cfg.DisableDockerMCP)
}

// MCPServersDisabled returns the MCP servers disabled for this
// repository. The override set is shared by every session, including
// sub-agent sessions.
func (o Ops) MCPServersDisabled(ctx context.Context) ([]string, error) {
	return o.App.Sessions.MCPDisabledServers(ctx)
}

// MCPServersEnabled returns the MCP servers with a repository-scoped
// enabled override: config-disabled servers the user enabled here.
// Startup force-starts them so the toggle survives restarts.
func (o Ops) MCPServersEnabled(ctx context.Context) ([]string, error) {
	return o.App.Sessions.MCPServersEnabled(ctx)
}

// SetMCPServerDisabled adds or removes a repository-scoped MCP override
// and applies it to the running client. Config files are never touched.
func (o Ops) SetMCPServerDisabled(ctx context.Context, name string, disabled bool) error {
	if err := o.App.Sessions.SetMCPServerDisabled(ctx, name, disabled); err != nil {
		return err
	}
	if err := mcptools.SetLocalDisabled(ctx, o.Cfg, name, disabled); err != nil {
		return err
	}
	o.configEvent()
	return nil
}

// SetMCPServerConfigDisabled toggles an MCP server's disabled flag in
// the global config and applies the change to the running client
// unless a repository-scoped override takes precedence.
func (o Ops) SetMCPServerConfigDisabled(ctx context.Context, name string, disabled bool) error {
	override, err := session.HasMCPOverride(ctx, o.App.Sessions, name)
	if err != nil {
		return err
	}
	if err := mcptools.SetConfigDisabled(ctx, o.Cfg, config.ScopeGlobal, name, disabled, override); err != nil {
		return err
	}
	o.configEvent()
	return nil
}

// StartMCPServer starts the named MCP server even when its config entry
// is disabled. The repository-scoped enabled override recorded by
// SetMCPServerDisabled makes the start survive restarts. Config files
// are never touched.
func (o Ops) StartMCPServer(ctx context.Context, name string) error {
	return mcptools.InitializeSingleForced(ctx, name, o.Cfg)
}

// RefreshMCPTools refreshes the tools of a named MCP server.
func (o Ops) RefreshMCPTools(ctx context.Context, name string) {
	mcptools.RefreshTools(ctx, o.Cfg, name)
}

// ReadMCPResource reads a resource from a named MCP server.
func (o Ops) ReadMCPResource(ctx context.Context, name, uri string) ([]proto.MCPResourceContents, error) {
	contents, err := mcptools.ReadResource(ctx, o.Cfg, name, uri)
	if err != nil {
		return nil, err
	}
	result := make([]proto.MCPResourceContents, len(contents))
	for i, c := range contents {
		result[i] = proto.MCPResourceContents{
			URI:      c.URI,
			MIMEType: c.MIMEType,
			Text:     c.Text,
			Blob:     c.Blob,
		}
	}
	return result, nil
}

// ListMCPPrompts returns the prompts every MCP server offers.
func (o Ops) ListMCPPrompts() ([]commands.MCPPrompt, error) {
	return commands.LoadMCPPrompts()
}

// GetMCPPrompt renders a prompt from a named MCP server.
func (o Ops) GetMCPPrompt(clientID, promptID string, args map[string]string) (string, error) {
	return commands.GetMCPPrompt(o.Cfg, clientID, promptID, args)
}

// MCPPendingAuth returns the MCP servers awaiting OAuth authentication.
func (o Ops) MCPPendingAuth() []mcptools.PendingAuthServer {
	return mcptools.PendingAuthMCPs(o.Cfg)
}
