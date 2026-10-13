package server

import (
	"net/http"

	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/rpc"
)

// requireName rejects an MCP request that does not name a server.
func requireName(name string) error {
	if name == "" {
		return badRequest("name is required")
	}
	return nil
}

// enableDockerMCP enables the Docker MCP server.
func (c *controllerV1) enableDockerMCP(r *http.Request, _ rpc.Empty) (rpc.Empty, error) {
	return rpc.Empty{}, c.backend.EnableDockerMCP(r.Context(), r.PathValue("id"))
}

// disableDockerMCP disables the Docker MCP server.
func (c *controllerV1) disableDockerMCP(r *http.Request, _ rpc.Empty) (rpc.Empty, error) {
	return rpc.Empty{}, c.backend.DisableDockerMCP(r.PathValue("id"))
}

// listMCPServersDisabled returns the MCP servers disabled for the
// workspace's repository.
func (c *controllerV1) listMCPServersDisabled(r *http.Request, _ rpc.Empty) ([]string, error) {
	return c.backend.MCPServersDisabled(r.Context(), r.PathValue("id"))
}

// setMCPServerDisabled toggles a repository-scoped MCP override.
func (c *controllerV1) setMCPServerDisabled(r *http.Request, req proto.MCPSetServerDisabledRequest) (rpc.Empty, error) {
	if err := requireName(req.Name); err != nil {
		return rpc.Empty{}, err
	}
	return rpc.Empty{}, c.backend.SetMCPServerDisabled(r.Context(), r.PathValue("id"), req.Name, req.Disabled)
}

// setMCPServerConfigDisabled toggles an MCP server's disabled flag in
// the global config.
func (c *controllerV1) setMCPServerConfigDisabled(r *http.Request, req proto.MCPSetServerDisabledRequest) (rpc.Empty, error) {
	if err := requireName(req.Name); err != nil {
		return rpc.Empty{}, err
	}
	return rpc.Empty{}, c.backend.SetMCPServerConfigDisabled(r.Context(), r.PathValue("id"), req.Name, req.Disabled)
}

// listMCPServersEnabled returns the MCP servers with a repository-scoped
// enabled override.
func (c *controllerV1) listMCPServersEnabled(r *http.Request, _ rpc.Empty) ([]string, error) {
	return c.backend.MCPServersEnabled(r.Context(), r.PathValue("id"))
}

// startMCPServer starts a named MCP server even when its config entry is
// disabled.
func (c *controllerV1) startMCPServer(r *http.Request, req proto.MCPNameRequest) (rpc.Empty, error) {
	if err := requireName(req.Name); err != nil {
		return rpc.Empty{}, err
	}
	return rpc.Empty{}, c.backend.StartMCPServer(r.Context(), r.PathValue("id"), req.Name)
}

// refreshMCPTools refreshes the tools of a named MCP server.
func (c *controllerV1) refreshMCPTools(r *http.Request, req proto.MCPNameRequest) (rpc.Empty, error) {
	return rpc.Empty{}, c.backend.RefreshMCPTools(r.Context(), r.PathValue("id"), req.Name)
}

// readMCPResource reads a resource from an MCP server.
func (c *controllerV1) readMCPResource(r *http.Request, req proto.MCPReadResourceRequest) ([]proto.MCPResourceContents, error) {
	return c.backend.ReadMCPResource(r.Context(), r.PathValue("id"), req.Name, req.URI)
}

// listMCPPrompts returns the prompts every MCP server offers.
func (c *controllerV1) listMCPPrompts(r *http.Request, _ rpc.Empty) ([]proto.MCPPrompt, error) {
	return c.backend.ListMCPPrompts(r.PathValue("id"))
}

// getMCPPrompt renders a prompt from an MCP server.
func (c *controllerV1) getMCPPrompt(r *http.Request, req proto.MCPGetPromptRequest) (proto.MCPGetPromptResponse, error) {
	prompt, err := c.backend.GetMCPPrompt(r.PathValue("id"), req.ClientID, req.PromptID, req.Args)
	return proto.MCPGetPromptResponse{Prompt: prompt}, err
}

// listMCPStates returns the state of every MCP client. MCP state is
// process-global, so the workspace is not looked up.
func (c *controllerV1) listMCPStates(r *http.Request, _ rpc.Empty) (map[string]proto.MCPClientInfo, error) {
	return proto.MCPClientInfosFromDomain(c.backend.MCPGetStates(r.PathValue("id"))), nil
}

// listMCPPendingAuth returns the MCP servers awaiting OAuth.
func (c *controllerV1) listMCPPendingAuth(r *http.Request, _ rpc.Empty) ([]proto.MCPPendingAuthServer, error) {
	pending, err := c.backend.MCPPendingAuth(r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	return proto.MCPPendingAuthFromDomain(pending), nil
}

// getMCPAuthURL returns the in-progress OAuth authorization URL of a
// named MCP server, if a flow is running.
func (c *controllerV1) getMCPAuthURL(r *http.Request, _ rpc.Empty) (proto.MCPAuthResponse, error) {
	name := r.URL.Query().Get("name")
	if err := requireName(name); err != nil {
		return proto.MCPAuthResponse{}, err
	}
	return proto.MCPAuthResponse{AuthURL: c.backend.MCPAuthURL(name)}, nil
}

// authenticateMCP runs the OAuth flow for a named MCP server. The local
// browser is suppressed on the server; the client polls pending-auth and
// auth-url to surface the authorization URL on the user's machine. The
// call blocks until the flow completes or the request context is
// cancelled (after which no response can be written).
func (c *controllerV1) authenticateMCP(r *http.Request, req proto.MCPNameRequest) (proto.MCPAuthResponse, error) {
	if err := c.backend.MCPAuthenticate(r.Context(), r.PathValue("id"), req.Name); err != nil {
		return proto.MCPAuthResponse{}, err
	}
	// The flow has finished by the time this returns, so there is no
	// in-progress authorization URL to report; the client polls
	// /mcp/auth-url for that while the flow runs.
	return proto.MCPAuthResponse{}, nil
}

// refreshMCPPrompts refreshes the prompts of a named MCP server.
func (c *controllerV1) refreshMCPPrompts(r *http.Request, req proto.MCPNameRequest) (rpc.Empty, error) {
	c.backend.MCPRefreshPrompts(r.Context(), r.PathValue("id"), req.Name)
	return rpc.Empty{}, nil
}

// refreshMCPResources refreshes the resources of a named MCP server.
func (c *controllerV1) refreshMCPResources(r *http.Request, req proto.MCPNameRequest) (rpc.Empty, error) {
	c.backend.MCPRefreshResources(r.Context(), r.PathValue("id"), req.Name)
	return rpc.Empty{}, nil
}
