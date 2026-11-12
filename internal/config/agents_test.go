package config

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// editingTools change files; the read-only agents (task and plan) never get
// them.
var editingTools = []string{"edit", "multiedit", "write", "download", "lsp_rename", "lsp_replace_symbol"}

// checkAgentTools sets up the agents with the given tools disabled and
// checks the permissions: no agent gets a disabled tool, the read-only
// agents get no editing tool, and the coder gets every other tool.
func checkAgentTools(t *testing.T, disabled []string) *Config {
	t.Helper()
	cfg := &Config{Options: &Options{DisabledTools: disabled}}
	cfg.SetupAgents()

	coder, ok := cfg.Agents[AgentCoder]
	require.True(t, ok)
	task, ok := cfg.Agents[AgentTask]
	require.True(t, ok)
	plan, ok := cfg.Agents[AgentPlan]
	require.True(t, ok)

	for _, agent := range []Agent{coder, task, plan} {
		for _, tool := range disabled {
			require.NotContains(t, agent.AllowedTools, tool, "%s got the disabled tool %s", agent.ID, tool)
		}
	}
	for _, agent := range []Agent{task, plan} {
		for _, tool := range editingTools {
			require.NotContains(t, agent.AllowedTools, tool, "read-only %s got %s", agent.ID, tool)
		}
	}
	for _, tool := range allToolNames() {
		if !slices.Contains(disabled, tool) {
			require.Contains(t, coder.AllowedTools, tool)
		}
	}
	return cfg
}

func TestConfig_setupAgentsWithNoDisabledTools(t *testing.T) {
	cfg := checkAgentTools(t, []string{})
	require.NotEmpty(t, cfg.Agents[AgentTask].AllowedTools)
	require.NotEmpty(t, cfg.Agents[AgentPlan].AllowedTools)
}

func TestConfig_setupAgentsWithDisabledTools(t *testing.T) {
	checkAgentTools(t, []string{"edit", "download", "grep"})
}

func TestConfig_setupAgentsWithEveryReadOnlyToolDisabled(t *testing.T) {
	cfg := checkAgentTools(t, []string{
		"agent",
		"agent_dispatch",
		"agent_send",
		"agent_status",
		"agent_stop",
		"glob",
		"grep",
		"ls",
		"lsp_call_hierarchy",
		"lsp_definition",
		"lsp_symbols",
		"sourcegraph",
		"view",
	})
	require.Equal(t, []string{"bash"}, cfg.Agents[AgentTask].AllowedTools)
	require.Equal(t, []string{"question"}, cfg.Agents[AgentPlan].AllowedTools)
}
