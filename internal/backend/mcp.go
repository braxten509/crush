package backend

import (
	"context"

	mcptools "github.com/charmbracelet/crush/internal/agent/tools/mcp"
	"github.com/charmbracelet/crush/internal/proto"
)

// EnableDockerMCP validates Docker MCP availability, stages the
// configuration, starts the MCP client, and persists the config.
func (b *Backend) EnableDockerMCP(ctx context.Context, workspaceID string) error {
	o, err := b.ops(workspaceID)
	if err != nil {
		return err
	}
	return o.EnableDockerMCP(ctx)
}

// DisableDockerMCP closes the Docker MCP client, removes the
// configuration, and persists the change.
func (b *Backend) DisableDockerMCP(workspaceID string) error {
	o, err := b.ops(workspaceID)
	if err != nil {
		return err
	}
	return o.DisableDockerMCP()
}

// MCPServersDisabled returns the MCP servers disabled for the given
// workspace's repository.
func (b *Backend) MCPServersDisabled(ctx context.Context, workspaceID string) ([]string, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return nil, err
	}
	return o.MCPServersDisabled(ctx)
}

// MCPServersEnabled returns the MCP servers with a repository-scoped
// enabled override (config-disabled servers the user enabled here).
func (b *Backend) MCPServersEnabled(ctx context.Context, workspaceID string) ([]string, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return nil, err
	}
	return o.MCPServersEnabled(ctx)
}

// SetMCPServerDisabled toggles a repository-scoped MCP override for the
// given workspace and applies it to the running client.
func (b *Backend) SetMCPServerDisabled(ctx context.Context, workspaceID, name string, disabled bool) error {
	o, err := b.ops(workspaceID)
	if err != nil {
		return err
	}
	return o.SetMCPServerDisabled(ctx, name, disabled)
}

// SetMCPServerConfigDisabled toggles an MCP server's disabled flag in the
// global config and applies the change to the running client.
func (b *Backend) SetMCPServerConfigDisabled(ctx context.Context, workspaceID, name string, disabled bool) error {
	o, err := b.ops(workspaceID)
	if err != nil {
		return err
	}
	return o.SetMCPServerConfigDisabled(ctx, name, disabled)
}

// StartMCPServer starts the named MCP server for the given workspace even
// when its config entry is disabled. Runtime-only.
func (b *Backend) StartMCPServer(ctx context.Context, workspaceID, name string) error {
	o, err := b.ops(workspaceID)
	if err != nil {
		return err
	}
	return o.StartMCPServer(ctx, name)
}

// RefreshMCPTools refreshes the tools for a named MCP server.
func (b *Backend) RefreshMCPTools(ctx context.Context, workspaceID, name string) error {
	o, err := b.ops(workspaceID)
	if err != nil {
		return err
	}
	o.RefreshMCPTools(ctx, name)
	return nil
}

// ReadMCPResource reads a resource from a named MCP server.
func (b *Backend) ReadMCPResource(ctx context.Context, workspaceID, name, uri string) ([]MCPResourceContents, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return nil, err
	}
	return o.ReadMCPResource(ctx, name, uri)
}

// GetMCPPrompt retrieves a prompt from a named MCP server.
func (b *Backend) GetMCPPrompt(workspaceID, clientID, promptID string, args map[string]string) (string, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return "", err
	}
	return o.GetMCPPrompt(clientID, promptID, args)
}

// ListMCPPrompts returns the prompts every MCP server offers.
func (b *Backend) ListMCPPrompts(workspaceID string) ([]proto.MCPPrompt, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return nil, err
	}
	prompts, err := o.ListMCPPrompts()
	if err != nil {
		return nil, err
	}
	return proto.MCPPromptsFromDomain(prompts), nil
}

// MCPGetStates returns the current state of all MCP clients. MCP state is
// process-global, so the workspace is not consulted.
func (b *Backend) MCPGetStates(_ string) map[string]mcptools.ClientInfo {
	return mcptools.GetStates()
}

// MCPRefreshPrompts refreshes prompts for a named MCP client.
func (b *Backend) MCPRefreshPrompts(ctx context.Context, _ string, name string) {
	mcptools.RefreshPrompts(ctx, name)
}

// MCPRefreshResources refreshes resources for a named MCP client.
func (b *Backend) MCPRefreshResources(ctx context.Context, _ string, name string) {
	mcptools.RefreshResources(ctx, name)
}

// MCPPendingAuth returns the MCP servers awaiting OAuth authentication,
// for clients that need to prompt the user. workspaceID selects the
// workspace whose config provides the server URLs.
func (b *Backend) MCPPendingAuth(workspaceID string) ([]mcptools.PendingAuthServer, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return nil, err
	}
	return o.MCPPendingAuth(), nil
}

// MCPAuthURL returns the current OAuth authorization URL for a named
// server, if a flow is in progress.
func (b *Backend) MCPAuthURL(name string) string {
	return mcptools.MCPAuthURL(name)
}

// MCPAuthenticate runs the OAuth flow for a named MCP server with the
// local browser suppressed: the authorization URL is exposed via
// MCPAuthURL/MCPPendingAuth for the calling client to open on the user's
// machine. The call blocks until the flow completes, fails, or ctx is
// cancelled. workspaceID selects the workspace whose config drives the
// flow.
func (b *Backend) MCPAuthenticate(ctx context.Context, workspaceID, name string) error {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return err
	}
	finish, cancel, err := mcptools.BeginAuth(ws.Cfg, name)
	if err != nil {
		return err
	}
	defer cancel()
	return finish(ctx)
}
