package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_setupAgentsWithNoDisabledTools(t *testing.T) {
	cfg := &Config{
		Options: &Options{
			DisabledTools: []string{},
		},
	}

	cfg.SetupAgents()
	coderAgent, ok := cfg.Agents[AgentCoder]
	require.True(t, ok)
	assert.Equal(t, allToolNames(), coderAgent.AllowedTools)

	taskAgent, ok := cfg.Agents[AgentTask]
	require.True(t, ok)
	assert.Equal(t, []string{"lsp_symbols", "lsp_definition", "lsp_call_hierarchy", "glob", "grep", "ls", "sourcegraph", "view"}, taskAgent.AllowedTools)

	planAgent, ok := cfg.Agents[AgentPlan]
	require.True(t, ok)
	assert.Equal(t, []string{"agent", "lsp_symbols", "lsp_definition", "lsp_call_hierarchy", "glob", "grep", "ls", "question", "sourcegraph", "view"}, planAgent.AllowedTools)
}

func TestConfig_setupAgentsWithDisabledTools(t *testing.T) {
	cfg := &Config{
		Options: &Options{
			DisabledTools: []string{
				"edit",
				"download",
				"grep",
			},
		},
	}

	cfg.SetupAgents()
	coderAgent, ok := cfg.Agents[AgentCoder]
	require.True(t, ok)

	assert.Equal(t, []string{"agent", "bash", "crush_info", "crush_logs", "job_output", "job_kill", "multiedit", "lsp_diagnostics", "lsp_references", "lsp_restart", "lsp_symbols", "lsp_definition", "lsp_call_hierarchy", "lsp_rename", "lsp_replace_symbol", "fetch", "agentic_fetch", "glob", "ls", "question", "sourcegraph", "todos", "view", "write", "list_mcp_resources", "read_mcp_resource"}, coderAgent.AllowedTools)

	taskAgent, ok := cfg.Agents[AgentTask]
	require.True(t, ok)
	assert.Equal(t, []string{"lsp_symbols", "lsp_definition", "lsp_call_hierarchy", "glob", "ls", "sourcegraph", "view"}, taskAgent.AllowedTools)

	planAgent, ok := cfg.Agents[AgentPlan]
	require.True(t, ok)
	assert.Equal(t, []string{"agent", "lsp_symbols", "lsp_definition", "lsp_call_hierarchy", "glob", "ls", "question", "sourcegraph", "view"}, planAgent.AllowedTools)
}

func TestConfig_setupAgentsWithEveryReadOnlyToolDisabled(t *testing.T) {
	cfg := &Config{
		Options: &Options{
			DisabledTools: []string{
				"agent",
				"glob",
				"grep",
				"ls",
				"lsp_call_hierarchy",
				"lsp_definition",
				"lsp_symbols",
				"sourcegraph",
				"view",
			},
		},
	}

	cfg.SetupAgents()
	coderAgent, ok := cfg.Agents[AgentCoder]
	require.True(t, ok)
	assert.Equal(t, []string{"bash", "crush_info", "crush_logs", "job_output", "job_kill", "download", "edit", "multiedit", "lsp_diagnostics", "lsp_references", "lsp_restart", "lsp_rename", "lsp_replace_symbol", "fetch", "agentic_fetch", "question", "todos", "write", "list_mcp_resources", "read_mcp_resource"}, coderAgent.AllowedTools)

	taskAgent, ok := cfg.Agents[AgentTask]
	require.True(t, ok)
	assert.Len(t, taskAgent.AllowedTools, 0)

	planAgent, ok := cfg.Agents[AgentPlan]
	require.True(t, ok)
	assert.Equal(t, []string{"question"}, planAgent.AllowedTools)
}
