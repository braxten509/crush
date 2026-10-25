package agent

import (
	"context"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/agent/tools/shelltool"
	"github.com/charmbracelet/crush/internal/agent/tools/webtool"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/hooks"
)

// buildTools builds the tool set of an agent: the built-in tools it is
// allowed, then the MCP tools it is allowed, sorted by name, guarded and
// wrapped with the PreToolUse hooks.
func (c *coordinator) buildTools(ctx context.Context, agent config.Agent, isSubAgent bool) ([]fantasy.AgentTool, error) {
	allTools, err := c.subAgentTools(ctx, agent)
	if err != nil {
		return nil, err
	}

	// Build hook runner if PreToolUse hooks are configured.
	var hookRunner *hooks.Runner
	if preToolHooks := c.cfg.Config().Hooks[hooks.EventPreToolUse]; len(preToolHooks) > 0 {
		hookRunner = hooks.NewRunner(preToolHooks, c.cfg.WorkingDir(), c.cfg.WorkingDir())
	}

	allTools = append(allTools, c.builtinTools(agent, isSubAgent)...)

	var filteredTools []fantasy.AgentTool
	for _, tool := range allTools {
		if slices.Contains(agent.AllowedTools, tool.Info().Name) {
			filteredTools = append(filteredTools, tool)
		}
	}
	filteredTools = append(filteredTools, c.allowedMCPTools(agent)...)
	slices.SortFunc(filteredTools, func(a, b fantasy.AgentTool) int {
		return strings.Compare(a.Info().Name, b.Info().Name)
	})

	// Wrap tools with hook interception for the top-level agent only.
	// Sub-agents (the `agent` task tool, `agentic_fetch`, etc.) run
	// without hook interception to avoid firing the user's hook N times
	// per delegated turn. The top-level invocation of the sub-agent tool
	// itself is still wrapped from the coder's side.
	// The secrets guard sits under the hooks and covers sub-agents too.
	filteredTools = wrapToolsWithHooks(guardTools(filteredTools), hookRunner, isSubAgent)

	return filteredTools, nil
}

// subAgentTools builds the tools that delegate to a sub-agent, when the
// agent is allowed them.
func (c *coordinator) subAgentTools(ctx context.Context, agent config.Agent) ([]fantasy.AgentTool, error) {
	var allTools []fantasy.AgentTool
	if slices.Contains(agent.AllowedTools, AgentToolName) {
		agentTool, err := c.agentTool(ctx)
		if err != nil {
			return nil, err
		}
		allTools = append(allTools, agentTool)
	}

	if slices.Contains(agent.AllowedTools, tools.AgenticFetchToolName) {
		agenticFetchTool, err := c.agenticFetchTool(ctx, nil)
		if err != nil {
			return nil, err
		}
		allTools = append(allTools, agenticFetchTool)
	}
	return allTools, nil
}

// builtinTools builds every built-in tool the agent could be allowed.
func (c *coordinator) builtinTools(agent config.Agent, isSubAgent bool) []fantasy.AgentTool {
	logFile := filepath.Join(c.cfg.Config().Options.DataDirectory, "logs", "crush.log")

	allTools := []fantasy.AgentTool{
		shelltool.NewBashTool(c.permissions, c.cfg.WorkingDir(), c.cfg.Config().Options.DataDirectory, c.cfg.Config().Options.Attribution, reportStartedJob),
		tools.NewCrushInfoTool(c.cfg, c.lspManager, c.allSkills, c.activeSkills, c.skillTracker),
		tools.NewCrushLogsTool(logFile),
		shelltool.NewJobOutputTool(c.cfg.Config().Options.DataDirectory),
		shelltool.NewJobKillTool(),
		webtool.NewDownloadTool(c.permissions, c.cfg.WorkingDir(), nil),
		tools.NewEditTool(c.lspManager, c.permissions, c.history, c.filetracker, c.cfg.WorkingDir()),
		tools.NewMultiEditTool(c.lspManager, c.permissions, c.history, c.filetracker, c.cfg.WorkingDir()),
		webtool.NewFetchTool(c.permissions, c.cfg.WorkingDir(), nil),
		tools.NewGlobTool(c.cfg.WorkingDir(), c.cfg.Config().Tools.Glob),
		tools.NewGrepTool(c.cfg.WorkingDir(), c.cfg.Config().Tools.Grep),
		tools.NewLsTool(c.permissions, c.cfg.WorkingDir(), c.cfg.Config().Tools.Ls),
		webtool.NewSourcegraphTool(nil),
		tools.NewTodosTool(c.sessions),
		tools.NewViewTool(c.lspManager, c.permissions, c.filetracker, c.skillTracker, c.cfg.WorkingDir(), c.cfg.Config().Options.SkillsPaths...),
		tools.NewWriteTool(c.lspManager, c.permissions, c.history, c.filetracker, c.cfg.WorkingDir()),
	}

	// Question tool is interactive-only and not available to sub-agents.
	if !isSubAgent && c.interactive {
		allTools = append(allTools, tools.NewQuestionTool(c.questions))
	}

	// Add LSP tools if user has configured LSPs or auto_lsp is enabled (nil or true).
	if len(c.cfg.Config().LSP) > 0 || c.cfg.Config().Options.AutoLSP == nil || *c.cfg.Config().Options.AutoLSP {
		allTools = append(
			allTools,
			tools.NewDiagnosticsTool(c.lspManager),
			tools.NewReferencesTool(c.lspManager),
			tools.NewLSPRestartTool(c.lspManager),
			tools.NewSymbolsTool(c.lspManager),
			tools.NewDefinitionTool(c.lspManager),
			tools.NewCallHierarchyTool(c.lspManager),
			tools.NewRenameTool(c.lspManager, c.permissions, c.history, c.filetracker, c.cfg.WorkingDir()),
			tools.NewReplaceSymbolTool(c.lspManager, c.permissions, c.history, c.filetracker, c.cfg.WorkingDir()),
		)
	}

	if len(c.cfg.Config().MCP) > 0 {
		allTools = append(
			allTools,
			tools.NewListMCPResourcesTool(c.cfg, c.permissions),
			tools.NewReadMCPResourceTool(c.cfg, c.permissions),
		)
	}
	return allTools
}

// allowedMCPTools returns the registered MCP tools the agent may use.
func (c *coordinator) allowedMCPTools(agent config.Agent) []fantasy.AgentTool {
	var allowed []fantasy.AgentTool
	for _, tool := range tools.GetMCPTools(c.permissions, c.cfg, c.cfg.WorkingDir()) {
		if agent.AllowedMCP == nil {
			// No MCP restrictions
			allowed = append(allowed, tool)
			continue
		}
		if len(agent.AllowedMCP) == 0 {
			// No MCPs allowed
			slog.Debug("No MCPs allowed", "tool", tool.Name(), "agent", agent.Name)
			break
		}

		for mcp, tools := range agent.AllowedMCP {
			if mcp != tool.MCP() {
				continue
			}
			if len(tools) == 0 || slices.Contains(tools, tool.MCPToolName()) {
				allowed = append(allowed, tool)
				break
			}
			slog.Debug("MCP not allowed", "tool", tool.Name(), "agent", agent.Name)
		}
	}
	return allowed
}
