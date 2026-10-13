package server

import (
	"github.com/charmbracelet/crush/internal/apigen"
	"github.com/charmbracelet/crush/internal/rpc"
)

// endpoints is the single source of truth for the v1 API: every entry
// drives both mux registration and the documentation served under
// /v1/docs/. Routes and body types come from the internal/rpc
// descriptors the client also uses; typed handlers are bound with route,
// and the few endpoints whose shape does not fit a typed handler (the
// SSE stream, queued-prompt listing and recall) keep hand-written ones.
func (c *controllerV1) endpoints() []apigen.Endpoint {
	return []apigen.Endpoint{
		route(c, rpc.Health, c.getHealth).
			Summary("Health check").
			Tags("system").
			Build(),

		route(c, rpc.Version, c.getVersion).
			Summary("Get server version").
			Tags("system").
			Build(),

		route(c, rpc.Control, c.postControl).
			Summary("Send server control command").
			Description("Accepts a control command (e.g. shutdown, shutdown_if_idle). "+
				"Shutdown is conditional: only the backend can rule on idleness without "+
				"racing a session that arrives between a client's own check and its request.").
			Tags("system").
			Fails(400, 409).
			Build(),

		route(c, rpc.ServerConfig, c.getConfig).
			Summary("Get server config").
			Tags("system").
			Build(),

		route(c, rpc.RetireClient, c.deleteClient).
			Summary("Retire a client").
			Description("Releases every claim the client holds.").
			Tags("system").
			PathParam("client_id", "Client ID (UUID)").
			Fails(400).
			Build(),

		route(c, rpc.ListWorkspaces, c.listWorkspaces).
			Summary("List workspaces").
			Tags("workspaces").
			Build(),

		route(c, rpc.CreateWorkspace, c.createWorkspace).
			Summary("Create workspace").
			Tags("workspaces").
			Fails(400, 500).
			Build(),

		route(c, rpc.GetWorkspace, c.getWorkspace).
			Summary("Get workspace").
			Tags("workspaces").
			PathParam("id", "Workspace ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.DeleteWorkspace, c.deleteWorkspace).
			Summary("Delete workspace").
			Tags("workspaces").
			PathParam("id", "Workspace ID").
			Fails(404).
			Build(),

		route(c, rpc.SetCurrentSession, c.setCurrentSession).
			Summary("Set current session for a client").
			Description("An empty session_id clears the entry (e.g. the client "+
				"is on the landing screen).").
			Tags("workspaces").
			PathParam("id", "Workspace ID").
			RequiredQueryParam("client_id", "Client ID (UUID)").
			Fails(400, 404).
			Build(),

		route(c, rpc.WorkspaceConfig, c.getWorkspaceConfig).
			Summary("Get workspace config").
			Description("Returns the resolved workspace configuration (config.Config).").
			Tags("workspaces").
			PathParam("id", "Workspace ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.WorkspaceProviders, c.getWorkspaceProviders).
			Summary("Get workspace providers").
			Description("Lists the providers available to the workspace.").
			Tags("workspaces").
			PathParam("id", "Workspace ID").
			Fails(404, 500).
			Build(),

		apigen.For(rpc.WorkspaceEvents).
			Summary("Stream workspace events (SSE)").
			Description("Streams workspace events as Server-Sent Events. The client must "+
				"identify itself with the client_id query parameter; the stream attaches "+
				"the client to the workspace until it disconnects.").
			Tags("workspaces").
			PathParam("id", "Workspace ID").
			RequiredQueryParam("client_id", "Client ID (UUID)").
			SSE().
			Fails(400, 404, 500).
			Handle(c.handleGetWorkspaceEvents),

		route(c, rpc.ListSavedSessions, c.listSavedSessions).
			Summary("List saved chats across projects").
			Tags("sessions").
			PathParam("id", "Workspace ID").
			Fails(404, 500).
			Build(),
		route(c, rpc.ChangeSavedSession, c.changeSavedSession, decodeErrorMessage("invalid saved chat")).
			Summary("Rename or delete a saved chat").
			Tags("sessions").
			PathParam("id", "Workspace ID").
			Fails(400, 404, 500).
			Build(),

		route(c, rpc.ListSessions, c.listSessions).
			Summary("List sessions").
			Tags("sessions").
			PathParam("id", "Workspace ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.CreateSession, c.createSession).
			Summary("Create session").
			Tags("sessions").
			PathParam("id", "Workspace ID").
			Fails(400, 404, 500).
			Build(),

		route(c, rpc.GetSession, c.getSession).
			Summary("Get session").
			Tags("sessions").
			PathParam("id", "Workspace ID").
			PathParam("sid", "Session ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.SaveSession, c.saveSession).
			Summary("Update session").
			Tags("sessions").
			PathParam("id", "Workspace ID").
			PathParam("sid", "Session ID").
			Fails(400, 404, 500).
			Build(),

		route(c, rpc.DeleteSession, c.deleteSession).
			Summary("Delete session").
			Tags("sessions").
			PathParam("id", "Workspace ID").
			PathParam("sid", "Session ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.SessionHistory, c.getSessionHistory).
			Summary("Get session history").
			Tags("sessions").
			PathParam("id", "Workspace ID").
			PathParam("sid", "Session ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.SessionMessages, c.getSessionMessages).
			Summary("Get session messages").
			Tags("sessions").
			PathParam("id", "Workspace ID").
			PathParam("sid", "Session ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.MessageReview, c.getMessageReview).
			Summary("Load retained file review details").
			Tags("sessions").
			PathParam("id", "Workspace ID").
			PathParam("sid", "Session ID").
			PathParam("mid", "Message ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.SessionUserMessages, c.getSessionUserMessages).
			Summary("Get user messages for session").
			Tags("sessions").
			PathParam("id", "Workspace ID").
			PathParam("sid", "Session ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.AllUserMessages, c.getAllUserMessages).
			Summary("Get all user messages for workspace").
			Tags("workspaces").
			PathParam("id", "Workspace ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.FileTrackerFiles, c.getFileTrackerFiles).
			Summary("List tracked files for session").
			Tags("filetracker").
			PathParam("id", "Workspace ID").
			PathParam("sid", "Session ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.FileTrackerRead, c.recordFileRead).
			Summary("Record file read").
			Tags("filetracker").
			PathParam("id", "Workspace ID").
			Fails(400, 404, 500).
			Build(),

		route(c, rpc.FileTrackerLastRead, c.getFileLastRead).
			Summary("Get last read time for file").
			Tags("filetracker").
			PathParam("id", "Workspace ID").
			QueryParam("session_id", "Session ID").
			RequiredQueryParam("path", "File path").
			Fails(404, 500).
			Build(),

		route(c, rpc.ListLSPs, c.listLSPs).
			Summary("List LSP clients").
			Tags("lsp").
			PathParam("id", "Workspace ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.LSPDiagnostics, c.getLSPDiagnostics).
			Summary("Get LSP diagnostics").
			Tags("lsp").
			PathParam("id", "Workspace ID").
			PathParam("lsp", "LSP client name").
			Fails(404, 500).
			Build(),

		route(c, rpc.LSPStart, c.startLSP).
			Summary("Start LSP server").
			Tags("lsp").
			PathParam("id", "Workspace ID").
			Fails(400, 404, 500).
			Build(),

		route(c, rpc.LSPStopAll, c.stopAllLSPs).
			Summary("Stop all LSP servers").
			Tags("lsp").
			PathParam("id", "Workspace ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.GetPermissionsSkip, c.getPermissionsSkip).
			Summary("Get skip permissions status").
			Tags("permissions").
			PathParam("id", "Workspace ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.SetPermissionsSkip, c.setPermissionsSkip).
			Summary("Set skip permissions").
			Tags("permissions").
			PathParam("id", "Workspace ID").
			Fails(400, 404, 500).
			Build(),

		route(c, rpc.GrantPermission, c.grantPermission).
			Summary("Grant permission").
			Tags("permissions").
			PathParam("id", "Workspace ID").
			Fails(400, 404, 500).
			Build(),

		route(c, rpc.AnswerQuestion, c.answerQuestion).
			Summary("Answer question batch").
			Tags("questions").
			PathParam("id", "Workspace ID").
			Fails(400, 404, 500).
			Build(),

		route(c, rpc.CancelQuestion, c.cancelQuestion).
			Summary("Cancel question batch").
			Tags("questions").
			PathParam("id", "Workspace ID").
			Fails(400, 404, 500).
			Build(),

		route(c, rpc.AgentInfo, c.getAgentInfo).
			Summary("Get agent info").
			Tags("agent").
			PathParam("id", "Workspace ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.SendMessage, c.sendMessage).
			Summary("Send message to agent").
			Description("Validates and accepts the prompt, then dispatches the run "+
				"detached from the requesting HTTP connection: the run survives client "+
				"disconnects and is only ended by the explicit cancel endpoint.").
			Tags("agent").
			PathParam("id", "Workspace ID").
			Fails(400, 404, 409, 500).
			Build(),

		route(c, rpc.InitAgent, c.initAgent, optionalBody()).
			Summary("Initialize agent").
			Description("The request body is optional.").
			Tags("agent").
			PathParam("id", "Workspace ID").
			Fails(400, 404, 500).
			Build(),

		route(c, rpc.UpdateAgent, c.updateAgent).
			Summary("Update agent").
			Tags("agent").
			PathParam("id", "Workspace ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.SetMainAgent, c.setMainAgent).
			Summary("Set main agent").
			Description(`Switches the workspace's active agent (e.g. "coder" or "plan").`).
			Tags("agent").
			PathParam("id", "Workspace ID").
			Fails(400, 404, 409, 500).
			Build(),

		route(c, rpc.AgentSession, c.getAgentSession).
			Summary("Get agent session").
			Tags("agent").
			PathParam("id", "Workspace ID").
			PathParam("sid", "Session ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.CancelAgent, c.cancelAgent).
			Summary("Cancel agent session").
			Tags("agent").
			PathParam("id", "Workspace ID").
			PathParam("sid", "Session ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.QueuedCount, c.getQueuedCount).
			Summary("Get queued prompt status").
			Description("Returns the number of queued prompts for the session.").
			Tags("agent").
			PathParam("id", "Workspace ID").
			PathParam("sid", "Session ID").
			Fails(404, 500).
			Build(),

		apigen.For(rpc.QueuedList).
			Summary("List queued prompts").
			Tags("agent").
			PathParam("id", "Workspace ID").
			PathParam("sid", "Session ID").
			QueryParam("details", "Return prompt text and submission IDs when true").
			Fails(404, 500).
			Handle(c.handleGetWorkspaceAgentSessionPromptList),

		route(c, rpc.ClearQueue, c.clearQueue).
			Summary("Clear prompt queue").
			Tags("agent").
			PathParam("id", "Workspace ID").
			PathParam("sid", "Session ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.SetKeptSkill, c.setKeptSkill).
			Summary("Turn a skill on or off for a whole session").
			Tags("sessions").
			PathParam("id", "Workspace ID").
			PathParam("sid", "Session ID").
			Fails(400, 404, 500).
			Build(),

		route(c, rpc.InterruptAgent, c.interruptAgent).
			Summary("Interrupt the active run and send the next queued prompt").
			Tags("agent").
			PathParam("id", "Workspace ID").
			PathParam("sid", "Session ID").
			Fails(404, 500).
			Build(),

		apigen.For(rpc.RecallQueued).
			Summary("Withdraw the newest queued prompt for editing").
			Tags("agent").
			PathParam("id", "Workspace ID").
			PathParam("sid", "Session ID").
			QueryParam("request_id", "Idempotent recall request token").
			QueryParam("acknowledge", "Release retained payload after successful delivery").
			Fails(400, 404, 500).
			Handle(c.handlePostWorkspaceAgentSessionPromptRecall),

		route(c, rpc.SummarizeSession, c.summarizeSession).
			Summary("Summarize session").
			Tags("agent").
			PathParam("id", "Workspace ID").
			PathParam("sid", "Session ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.RunShellCommand, c.runShellCommand).
			Summary("Run shell command").
			Description("Runs a shell command in the workspace on behalf of the session.").
			Tags("agent").
			PathParam("id", "Workspace ID").
			PathParam("sid", "Session ID").
			Fails(400, 404, 500).
			Build(),

		route(c, rpc.DefaultSmallModel, c.getDefaultSmallModel).
			Summary("Get default small model").
			Description("Returns the default small model (config.SelectedModel) for the provider.").
			Tags("agent").
			PathParam("id", "Workspace ID").
			QueryParam("provider_id", "Provider ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.SetConfigField, c.setConfigField).
			Summary("Set a config field").
			Tags("config").
			PathParam("id", "Workspace ID").
			Fails(400, 404, 500).
			Build(),

		route(c, rpc.RemoveConfigField, c.removeConfigField).
			Summary("Remove a config field").
			Tags("config").
			PathParam("id", "Workspace ID").
			Fails(400, 404, 500).
			Build(),

		route(c, rpc.UpdatePreferredModel, c.updatePreferredModel).
			Summary("Set the preferred model").
			Tags("config").
			PathParam("id", "Workspace ID").
			Fails(400, 404, 500).
			Build(),

		route(c, rpc.SetCompactMode, c.setCompactMode).
			Summary("Set compact mode").
			Tags("config").
			PathParam("id", "Workspace ID").
			Fails(400, 404, 500).
			Build(),

		route(c, rpc.SetProviderAPIKey, c.setProviderAPIKey).
			Summary("Set provider API key").
			Tags("config").
			PathParam("id", "Workspace ID").
			Fails(400, 404, 500).
			Build(),

		route(c, rpc.ImportCopilot, c.importCopilot).
			Summary("Import Copilot credentials").
			Tags("config").
			PathParam("id", "Workspace ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.RefreshOAuthToken, c.refreshOAuthToken).
			Summary("Refresh OAuth token").
			Tags("config").
			PathParam("id", "Workspace ID").
			Fails(400, 404, 500).
			Build(),

		route(c, rpc.ProjectNeedsInit, c.getProjectNeedsInit).
			Summary("Check if project needs initialization").
			Tags("project").
			PathParam("id", "Workspace ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.ProjectInit, c.markProjectInitialized).
			Summary("Mark project as initialized").
			Tags("project").
			PathParam("id", "Workspace ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.ProjectInitPrompt, c.getProjectInitPrompt).
			Summary("Get project initialization prompt").
			Tags("project").
			PathParam("id", "Workspace ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.GitBranch, c.getGitBranch).
			Summary("Get current Git branch").
			Tags("git").
			PathParam("id", "Workspace ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.ListSkills, c.listSkills).
			Summary("List visible skills").
			Tags("skills").
			PathParam("id", "Workspace ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.ReadSkill, c.readSkill).
			Summary("Read skill content").
			Tags("skills").
			PathParam("id", "Workspace ID").
			Fails(400, 404, 500).
			Build(),

		route(c, rpc.InstalledSkills, c.listInstalledSkills).
			Summary("List installed skills including disabled skills").
			Tags("skills").PathParam("id", "Workspace ID").
			Fails(404, 500).Build(),

		route(c, rpc.ManageSkill, c.manageSkill, limitBody(64<<10, "invalid skill request")).
			Summary("Install or enable/disable a skill").
			Tags("skills").PathParam("id", "Workspace ID").
			Fails(400, 404, 500).Build(),

		route(c, rpc.RefreshMCPTools, c.refreshMCPTools).
			Summary("Refresh MCP tools").
			Tags("mcp").
			PathParam("id", "Workspace ID").
			Fails(400, 404, 500).
			Build(),

		route(c, rpc.ReadMCPResource, c.readMCPResource).
			Summary("Read MCP resource").
			Tags("mcp").
			PathParam("id", "Workspace ID").
			Fails(400, 404, 500).
			Build(),

		route(c, rpc.ListMCPPrompts, c.listMCPPrompts).
			Summary("Get MCP prompts").
			Tags("mcp").
			PathParam("id", "Workspace ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.GetMCPPrompt, c.getMCPPrompt).
			Summary("Get MCP prompt").
			Tags("mcp").
			PathParam("id", "Workspace ID").
			Fails(400, 404, 500).
			Build(),

		route(c, rpc.MCPStates, c.listMCPStates).
			Summary("Get MCP client states").
			Tags("mcp").
			PathParam("id", "Workspace ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.MCPPendingAuth, c.listMCPPendingAuth).
			Summary("Get MCP servers pending OAuth").
			Tags("mcp").
			PathParam("id", "Workspace ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.MCPAuthURL, c.getMCPAuthURL).
			Summary("Get MCP OAuth authorization URL").
			Description("Returns the current OAuth authorization URL for a named MCP "+
				"server, if a flow is in progress.").
			Tags("mcp").
			PathParam("id", "Workspace ID").
			RequiredQueryParam("name", "MCP server name").
			Fails(400).
			Build(),

		route(c, rpc.MCPAuthenticate, c.authenticateMCP).
			Summary("Authenticate an MCP server").
			Description("Runs the OAuth flow for a named MCP server. The local browser "+
				"is suppressed on the server; the client polls pending-auth and auth-url "+
				"to surface the authorization URL. The call blocks until the flow "+
				"completes or the request context is cancelled.").
			Tags("mcp").
			PathParam("id", "Workspace ID").
			Fails(400, 404, 500).
			Build(),

		route(c, rpc.MCPRefreshPrompts, c.refreshMCPPrompts).
			Summary("Refresh MCP prompts").
			Tags("mcp").
			PathParam("id", "Workspace ID").
			Fails(400, 404, 500).
			Build(),

		route(c, rpc.MCPRefreshResources, c.refreshMCPResources).
			Summary("Refresh MCP resources").
			Tags("mcp").
			PathParam("id", "Workspace ID").
			Fails(400, 404, 500).
			Build(),

		route(c, rpc.EnableDockerMCP, c.enableDockerMCP).
			Summary("Enable Docker MCP").
			Tags("mcp").
			PathParam("id", "Workspace ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.DisableDockerMCP, c.disableDockerMCP).
			Summary("Disable Docker MCP").
			Tags("mcp").
			PathParam("id", "Workspace ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.MCPServersDisabled, c.listMCPServersDisabled).
			Summary("List disabled MCP servers").
			Tags("mcp").
			PathParam("id", "Workspace ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.SetMCPServerDisabled, c.setMCPServerDisabled).
			Summary("Toggle an MCP server for the repository").
			Tags("mcp").
			PathParam("id", "Workspace ID").
			Fails(400, 404, 500).
			Build(),

		route(c, rpc.MCPServersEnabled, c.listMCPServersEnabled).
			Summary("List enabled-override MCP servers").
			Tags("mcp").
			PathParam("id", "Workspace ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.SetMCPServerConfigDisabled, c.setMCPServerConfigDisabled).
			Summary("Toggle an MCP server in the config").
			Tags("mcp").
			PathParam("id", "Workspace ID").
			Fails(400, 404, 500).
			Build(),

		route(c, rpc.StartMCPServer, c.startMCPServer).
			Summary("Start an MCP server").
			Tags("mcp").
			PathParam("id", "Workspace ID").
			Fails(400, 404, 500).
			Build(),
	}
}
