package agent

import (
	"context"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

// Agent CLI sub-agents start without the user's MCP add-ons unless the
// spawn asked for them (owner, 2026-10-30).
func TestCLISubAgentMCPFollowsRequest(t *testing.T) {
	env := testEnv(t)
	providerCfg := config.ProviderConfig{ID: "claude", Type: config.TypeClaudeCode, Models: []catwalk.Model{{ID: "opus"}}}
	coord := newTestCoordinator(t, env, providerCfg.ID, providerCfg)
	coord.permissions = env.permissions
	h := &taskHub{c: coord, dir: t.TempDir()}
	selected := config.SelectedModel{Provider: providerCfg.ID, Model: "opus"}
	for _, mcp := range []bool{false, true} {
		_, m, err := h.subAgent(t.Context(), providerCfg, providerCfg.Models[0], selected, false, mcp)
		require.NoError(t, err)
		cm, ok := m.Model.(*cliagent.Model)
		require.True(t, ok)
		require.Equal(t, !mcp, cm.NoMCP)
	}
}

// A follow-up keeps the task's add-on setting; --mcp turns it on, also
// when switching a stopped task to another model.
func TestContinueKeepsMCPSetting(t *testing.T) {
	c := sessionRunTestCoordinator(t, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		return fakeSessionReply(call, "received"), nil
	})
	h := resumeTestHub(t, c)
	c.tasks = h
	claude := config.ProviderConfig{ID: "claude", Type: config.TypeClaudeCode, Models: []catwalk.Model{{ID: "opus"}, {ID: "sonnet"}}}
	c.cfg.Config().Providers.Set(claude.ID, claude)
	parent, _ := helperChats(t, c)
	h.subAgentHook = func(config.ProviderConfig, catwalk.Model) SessionAgent {
		return newMockAgent("claude", 4096, func(context.Context, SessionAgentCall) (*fantasy.AgentResult, error) {
			return agentResultWithText("done"), nil
		})
	}
	idle := func(id string) {
		t.Helper()
		require.Eventually(t, func() bool { return !h.childRunning(id) }, 5*time.Second, 10*time.Millisecond)
	}

	task, err := h.spawn(TaskRequest{Session: parent.ID, CLI: "claude", Prompt: "work"})
	require.NoError(t, err)
	require.False(t, task.MCP)
	idle(task.ChildID)
	next, err := h.continueTask(TaskRequest{Session: parent.ID, Continue: task.ID, Model: "sonnet", MCP: true, Prompt: "more"})
	require.NoError(t, err)
	require.True(t, next.MCP, "--mcp turns the add-ons on for a switched task")
	idle(task.ChildID)
	next, err = h.continueTask(TaskRequest{Session: parent.ID, Continue: task.ID, Prompt: "more"})
	require.NoError(t, err)
	require.True(t, next.MCP, "a follow-up keeps the earlier setting")
}
