package agent

import (
	"context"
	"log/slog"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/tools/mcp"
)

// filterToolsForChannel scopes the tool list for a turn. A channel-originated
// turn (channel != "") sees only the originating channel server's tools plus
// all non-channel tools — the model's reach is restricted to the channel it
// is replying through, so it cannot accidentally send via a different
// messaging backend. A local turn (channel == "") keeps every tool,
// including channel server tools, so a user in the TUI can still ask the
// agent to send a message through Signal or any other enabled channel.
func filterToolsForChannel(agentTools []fantasy.AgentTool, channel string, states map[string]mcp.ClientInfo) []fantasy.AgentTool {
	if channel == "" {
		return agentTools
	}
	filtered := make([]fantasy.AgentTool, 0, len(agentTools))
	for _, agentTool := range agentTools {
		mcpName, ok := toolMCPName(agentTool)
		if !ok {
			filtered = append(filtered, agentTool)
			continue
		}
		state, found := states[mcpName]
		if !found || !state.Channel || channel == mcpName {
			filtered = append(filtered, agentTool)
		}
	}
	return filtered
}

// toolMCPName looks through safety wrappers without removing them from the
// tool list. Both channel routing and repository toggles need the server name.
func toolMCPName(tool fantasy.AgentTool) (string, bool) {
	for {
		switch wrapped := tool.(type) {
		case guardedTool:
			tool = wrapped.AgentTool
		case *hookedTool:
			tool = wrapped.inner
		default:
			mcpTool, ok := tool.(interface{ MCP() string })
			if !ok {
				return "", false
			}
			return mcpTool.MCP(), true
		}
	}
}

// filterDisabledMCPTools removes tools from MCP servers disabled via the
// "Toggle MCPs" dialog. The override set is repository-scoped and shared
// by every session in the repository, including sub-agent sessions.
// Connections are process-global and left untouched; only the tool list
// changes.
func (a *sessionAgent) filterDisabledMCPTools(ctx context.Context, toolList []fantasy.AgentTool) []fantasy.AgentTool {
	disabledServers, err := a.sessions.MCPDisabledServers(ctx)
	if err != nil {
		slog.Error("Failed to list disabled MCP servers", "error", err)
		return toolList
	}
	if len(disabledServers) == 0 {
		return toolList
	}
	disabled := make(map[string]struct{}, len(disabledServers))
	for _, name := range disabledServers {
		disabled[name] = struct{}{}
	}
	filtered := make([]fantasy.AgentTool, 0, len(toolList))
	for _, t := range toolList {
		if mcpName, ok := toolMCPName(t); ok {
			if _, off := disabled[mcpName]; off {
				continue
			}
		}
		filtered = append(filtered, t)
	}
	return filtered
}
