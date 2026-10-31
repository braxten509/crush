package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/hyper"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

func TestTaskInheritsCurrentProviderAccount(t *testing.T) {
	c := sessionRunTestCoordinator(t, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		return fakeSessionReply(call, "received"), nil
	})
	h := resumeTestHub(t, c)
	c.tasks = h
	provider := config.ProviderConfig{ID: "claude", Type: config.TypeClaudeCode, Models: []catwalk.Model{{ID: "opus"}}}
	c.cfg.Config().Providers.Set(provider.ID, provider)
	// The parent chat runs on its own account, which its workers follow
	// (owner, 2026-10-24: no shared model anywhere).
	parent, _ := helperChats(t, c)
	setChatModel(t, c, parent.ID, config.SelectedModel{Provider: provider.ID, Model: "opus", Account: "claude-2"})
	h.subAgentHook = func(config.ProviderConfig, catwalk.Model) SessionAgent {
		return newMockAgent("claude", 4096, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
			return agentResultWithText("done"), nil
		})
	}
	task, err := h.spawn(TaskRequest{Session: parent.ID, CLI: "claude", Prompt: "work"})
	require.NoError(t, err)
	require.Equal(t, "claude-2", task.Account)
	require.Equal(t, "claude-2", chatModelOf(t, c, task.ChildID).Account, "the worker's chat keeps the worker's model")
	require.Equal(t, "claude-2", h.taskAccount(parent.ID, provider, ""))
	cur := config.SelectedModel{Provider: provider.ID, Model: "opus"}
	next, ok := h.subAgentFallback(t.Context(), task, cur, cur, errOut)
	require.True(t, ok, "a worker hitting its old account's limit follows the parent's new account")
	require.Equal(t, "claude-2", next.model.ModelCfg.Account)
	setChatModel(t, c, parent.ID, config.SelectedModel{Provider: provider.ID, Model: "opus"})
	require.Empty(t, h.taskAccount(parent.ID, provider, "claude-2"), "returning to the default account also follows")
	require.Equal(t, "codex-2", h.taskAccount(parent.ID, config.ProviderConfig{Type: config.TypeCodexCLI}, "codex-2"), "other providers keep their own account")
}

// Background tasks build their models through the same path as the main
// agents, so they get the whole decorator chain and the provider's prompt
// prefix and auto-summarize setting.
func TestTaskSubAgentBuildsModelLikeCoordinator(t *testing.T) {
	env := testEnv(t)
	providerCfg := config.ProviderConfig{
		ID:                 hyper.Name,
		Type:               catwalk.TypeOpenAICompat,
		APIKey:             "test-key",
		SystemPromptPrefix: "prefix for every prompt",
	}
	coord := newTestCoordinator(t, env, hyper.Name, providerCfg)
	coord.permissions = env.permissions
	coord.cfg.Config().Options.DisableAutoSummarize = true

	model := catwalk.Model{ID: "test-model", ContextWindow: 100_000}
	selected := config.SelectedModel{Provider: hyper.Name, Model: model.ID}
	h := &taskHub{c: coord, dir: t.TempDir()}

	sub, m, err := h.subAgent(t.Context(), providerCfg, model, selected, false, false, "")
	require.NoError(t, err)

	// The decorators are the providers package's; their types name them.
	require.Equal(t, "*providers.hyperCreditsModel", fmt.Sprintf("%T", m.Model),
		"hyper task models refresh the credit balance")

	agent, ok := sub.(*sessionAgent)
	require.True(t, ok)
	require.Equal(t, providerCfg.SystemPromptPrefix, agent.systemPromptPrefix.Get())
	require.True(t, agent.disableAutoSummarize)
	require.Same(t, m.Model, agent.model.Get().Model)
}

// An API sub-agent that isn't read-only must be able to change files and run
// commands; a read-only one runs commands too, but locked so they can't
// change anything.
func TestAPISubAgentToolsFollowReadOnly(t *testing.T) {
	env := testEnv(t)
	providerCfg := config.ProviderConfig{ID: hyper.Name, Type: catwalk.TypeOpenAICompat, APIKey: "test-key"}
	coord := newTestCoordinator(t, env, hyper.Name, providerCfg)
	coord.permissions = env.permissions
	coord.cfg.Config().SetupAgents()
	model := catwalk.Model{ID: "test-model", ContextWindow: 100_000}
	selected := config.SelectedModel{Provider: hyper.Name, Model: model.ID}
	h := &taskHub{c: coord, dir: t.TempDir()}

	toolsOf := func(readOnly bool) map[string]fantasy.AgentTool {
		sub, _, err := h.subAgent(t.Context(), providerCfg, model, selected, readOnly, false, "")
		require.NoError(t, err)
		out := map[string]fantasy.AgentTool{}
		for _, tool := range sub.(*sessionAgent).tools.Copy() {
			out[tool.Info().Name] = tool
		}
		return out
	}
	names := func(readOnly bool) []string {
		return slices.Collect(maps.Keys(toolsOf(readOnly)))
	}

	writer := names(false)
	for _, want := range []string{"bash", "edit", "multiedit", "write", "view", "grep", "job_output"} {
		require.Contains(t, writer, want)
	}
	for _, banned := range []string{"agent", "agentic_fetch", "question"} {
		require.NotContains(t, writer, banned)
	}

	reader := names(true)
	for _, want := range []string{"bash", "job_output", "job_kill", "view", "grep"} {
		require.Contains(t, reader, want)
	}
	for _, banned := range []string{"edit", "multiedit", "write", "download"} {
		require.NotContains(t, reader, banned)
	}

	// The reader's commands run locked: this project sits in the temporary
	// folder, which the lock can't protect, so nothing runs at all.
	input, err := json.Marshal(tools.BashParams{Command: "touch locked-file", Description: "write attempt"})
	require.NoError(t, err)
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, "read-only-test")
	_, err = toolsOf(true)["bash"].Run(ctx, fantasy.ToolCall{ID: "call", Name: "bash", Input: string(input)})
	require.NoError(t, err)
	require.NoFileExists(t, filepath.Join(coord.cfg.WorkingDir(), "locked-file"))
}
