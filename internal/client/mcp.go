package client

import (
	"context"
	"net/url"

	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/rpc"
	"github.com/charmbracelet/x/powernap/pkg/lsp/protocol"
)

// MCPResourceContents holds the contents of an MCP resource.
type MCPResourceContents = proto.MCPResourceContents

// MCPGetStates retrieves the MCP client states for a workspace.
func (c *Client) MCPGetStates(ctx context.Context, id string) (map[string]proto.MCPClientInfo, error) {
	return newCall(c, rpc.MCPStates, "get MCP states", "MCP states").at(id).get(ctx)
}

// MCPPendingAuth retrieves the MCP servers awaiting OAuth authentication
// for a workspace.
func (c *Client) MCPPendingAuth(ctx context.Context, id string) ([]proto.MCPPendingAuthServer, error) {
	return newCall(c, rpc.MCPPendingAuth, "get MCP pending auth", "MCP pending auth").at(id).get(ctx)
}

// MCPAuthURL retrieves the current OAuth authorization URL for a named MCP
// server, if a flow is in progress.
func (c *Client) MCPAuthURL(ctx context.Context, id, name string) (string, error) {
	resp, err := newCall(c, rpc.MCPAuthURL, "get MCP auth URL", "MCP auth URL").at(id).
		with(url.Values{"name": []string{name}}).get(ctx)
	return resp.AuthURL, err
}

// MCPAuthenticate runs the OAuth flow for a named MCP server. The server's
// local browser is suppressed; the caller is responsible for surfacing the
// authorization URL (via polling [Client.MCPPendingAuth] / state events)
// and opening it on the user's machine. The call blocks until the flow
// completes, fails, or ctx is cancelled.
func (c *Client) MCPAuthenticate(ctx context.Context, id, name string) error {
	_, err := newCall(c, rpc.MCPAuthenticate, "authenticate MCP", "MCP auth response").at(id).
		do(ctx, proto.MCPNameRequest{Name: name})
	return err
}

// MCPRefreshPrompts refreshes prompts for a named MCP client.
func (c *Client) MCPRefreshPrompts(ctx context.Context, id, name string) error {
	_, err := newCall(c, rpc.MCPRefreshPrompts, "refresh MCP prompts", "").at(id).
		do(ctx, proto.MCPNameRequest{Name: name})
	return err
}

// MCPRefreshResources refreshes resources for a named MCP client.
func (c *Client) MCPRefreshResources(ctx context.Context, id, name string) error {
	_, err := newCall(c, rpc.MCPRefreshResources, "refresh MCP resources", "").at(id).
		do(ctx, proto.MCPNameRequest{Name: name})
	return err
}

// EnableDockerMCP enables the Docker MCP server on the workspace.
func (c *Client) EnableDockerMCP(ctx context.Context, id string) error {
	_, err := newCall(c, rpc.EnableDockerMCP, "enable docker MCP", "").at(id).get(ctx)
	return err
}

// DisableDockerMCP disables the Docker MCP server on the workspace.
func (c *Client) DisableDockerMCP(ctx context.Context, id string) error {
	_, err := newCall(c, rpc.DisableDockerMCP, "disable docker MCP", "").at(id).get(ctx)
	return err
}

// MCPServersDisabled returns the MCP servers disabled for the workspace's
// repository.
func (c *Client) MCPServersDisabled(ctx context.Context, id string) ([]string, error) {
	return newCall(c, rpc.MCPServersDisabled, "get disabled MCP servers", "disabled MCP servers").at(id).get(ctx)
}

// MCPServersEnabled returns the MCP servers with a repository-scoped
// enabled override for the workspace's repository.
func (c *Client) MCPServersEnabled(ctx context.Context, id string) ([]string, error) {
	return newCall(c, rpc.MCPServersEnabled, "get enabled MCP servers", "enabled MCP servers").at(id).get(ctx)
}

// SetMCPServerDisabled toggles a repository-scoped MCP override on the
// workspace.
func (c *Client) SetMCPServerDisabled(ctx context.Context, id, name string, disabled bool) error {
	_, err := newCall(c, rpc.SetMCPServerDisabled, "set disabled MCP server", "").at(id).
		do(ctx, proto.MCPSetServerDisabledRequest{Name: name, Disabled: disabled})
	return err
}

// StartMCPServer starts a named MCP server on the workspace even when its
// config entry is disabled.
func (c *Client) StartMCPServer(ctx context.Context, id, name string) error {
	_, err := newCall(c, rpc.StartMCPServer, "start MCP server", "").at(id).
		do(ctx, proto.MCPNameRequest{Name: name})
	return err
}

// SetMCPServerConfigDisabled toggles an MCP server's disabled flag in the
// global config on the workspace's server.
func (c *Client) SetMCPServerConfigDisabled(ctx context.Context, id, name string, disabled bool) error {
	_, err := newCall(c, rpc.SetMCPServerConfigDisabled, "set config-disabled MCP server", "").at(id).
		do(ctx, proto.MCPSetServerDisabledRequest{Name: name, Disabled: disabled})
	return err
}

// RefreshMCPTools refreshes tools for a named MCP server.
func (c *Client) RefreshMCPTools(ctx context.Context, id, name string) error {
	_, err := newCall(c, rpc.RefreshMCPTools, "refresh MCP tools", "").at(id).
		do(ctx, proto.MCPNameRequest{Name: name})
	return err
}

// ReadMCPResource reads a resource from a named MCP server.
func (c *Client) ReadMCPResource(ctx context.Context, id, name, uri string) ([]MCPResourceContents, error) {
	return newCall(c, rpc.ReadMCPResource, "read MCP resource", "MCP resource").at(id).
		do(ctx, proto.MCPReadResourceRequest{Name: name, URI: uri})
}

// ListMCPPrompts retrieves the prompts every MCP server offers.
func (c *Client) ListMCPPrompts(ctx context.Context, id string) ([]proto.MCPPrompt, error) {
	return newCall(c, rpc.ListMCPPrompts, "list MCP prompts", "MCP prompts").at(id).get(ctx)
}

// GetMCPPrompt retrieves a prompt from a named MCP server.
func (c *Client) GetMCPPrompt(ctx context.Context, id, clientID, promptID string, args map[string]string) (string, error) {
	result, err := newCall(c, rpc.GetMCPPrompt, "get MCP prompt", "MCP prompt response").at(id).
		do(ctx, proto.MCPGetPromptRequest{ClientID: clientID, PromptID: promptID, Args: args})
	return result.Prompt, err
}

// GetLSPs retrieves the LSP client states for a workspace.
func (c *Client) GetLSPs(ctx context.Context, id string) (map[string]proto.LSPClientInfo, error) {
	return newCall(c, rpc.ListLSPs, "get LSPs", "LSPs").at(id).get(ctx)
}

// GetLSPDiagnostics retrieves LSP diagnostics for a specific LSP client.
func (c *Client) GetLSPDiagnostics(ctx context.Context, id string, lspName string) (map[protocol.DocumentURI][]protocol.Diagnostic, error) {
	return newCall(c, rpc.LSPDiagnostics, "get LSP diagnostics", "LSP diagnostics").at(id, lspName).get(ctx)
}

// LSPStart starts an LSP server for a path.
func (c *Client) LSPStart(ctx context.Context, id string, path string) error {
	_, err := newCall(c, rpc.LSPStart, "start LSP", "").at(id).do(ctx, proto.LSPStartRequest{Path: path})
	return err
}

// LSPStopAll stops all LSP servers for a workspace.
func (c *Client) LSPStopAll(ctx context.Context, id string) error {
	_, err := newCall(c, rpc.LSPStopAll, "stop LSPs", "").at(id).get(ctx)
	return err
}
