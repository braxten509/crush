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
// wrapped with the tool hooks.
func (c *coordinator) buildTools(ctx context.Context, agent config.Agent, isSubAgent bool) ([]fantasy.AgentTool, error) {
	allowed := c.buildableToolNames(agent, isSubAgent)
	allTools, err := c.subAgentTools(ctx, agent, allowed)
	if err != nil {
		return nil, err
	}

	allTools = append(allTools, c.builtinTools(allowed)...)

	var filteredTools []fantasy.AgentTool
	for _, tool := range allTools {
		if slices.Contains(allowed, tool.Info().Name) {
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
	filteredTools = wrapToolsWithHooks(guardTools(filteredTools), c.toolHookRunner(), isSubAgent)

	return filteredTools, nil
}

// toolHookRunner is the runner for tool hooks: the one the app made, or one
// reading the hooks from the live config, so a hook added later applies to
// the next tool build either way.
func (c *coordinator) toolHookRunner() *hooks.Runner {
	if c.hookRunner != nil {
		return c.hookRunner
	}
	return hooks.NewRunner(func() map[string][]hooks.HookConfig {
		return c.cfg.Config().Hooks
	}, c.cfg.WorkingDir(), c.cfg.WorkingDir())
}

// subAgentTools builds the tools that delegate to a sub-agent, when the
// agent is allowed them.
func (c *coordinator) subAgentTools(ctx context.Context, agent config.Agent, allowed []string) ([]fantasy.AgentTool, error) {
	var allTools []fantasy.AgentTool
	if slices.Contains(allowed, AgentToolName) {
		agentTool, err := c.agentTool(ctx, agent.ID)
		if err != nil {
			return nil, err
		}
		allTools = append(allTools, agentTool)
	}
	if slices.Contains(allowed, AgentDispatchToolName) {
		dispatchTool, err := c.agentDispatchTool(ctx, agent.ID)
		if err != nil {
			return nil, err
		}
		allTools = append(allTools, dispatchTool)
	}
	if slices.Contains(allowed, AgentSendToolName) {
		allTools = append(allTools, c.agentSendTool())
	}
	if slices.Contains(allowed, AgentStatusToolName) {
		allTools = append(allTools, c.agentStatusTool())
	}
	if slices.Contains(allowed, AgentStopToolName) {
		allTools = append(allTools, c.agentStopTool())
	}

	if slices.Contains(allowed, tools.AgenticFetchToolName) {
		agenticFetchTool, err := c.agenticFetchTool(ctx, nil)
		if err != nil {
			return nil, err
		}
		allTools = append(allTools, agenticFetchTool)
	}
	return allTools, nil
}

// builtinTools builds every built-in tool the agent could be allowed.
func (c *coordinator) builtinTools(allowed []string) []fantasy.AgentTool {
	cfg := c.cfg.Config()
	logFile := filepath.Join(cfg.Options.DataDirectory, "logs", "crush.log")

	allTools := []fantasy.AgentTool{
		shelltool.NewBashTool(c.permissions, c.cfg.WorkingDir(), cfg.Options.DataDirectory, cfg.Options.Attribution, cfg.Options.GetMaxBackgroundJobs(), cfg.Permissions, reportStartedJob),
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

	// Interactive-only, and never given to sub-agents; effectiveToolNames
	// is what decides both.
	if slices.Contains(allowed, tools.QuestionToolName) {
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
