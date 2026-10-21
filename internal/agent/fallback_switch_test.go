package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

// fallbackCoordinator is a coordinator whose chats run on Claude and Codex
// accounts under the given order, reading usage from fake fetchers.
func fallbackCoordinator(t *testing.T, blocks []config.FallbackBlock, usage map[string]float64, refills map[string]int) (*coordinator, string) {
	t.Helper()
	t.Setenv("CRUSH_GLOBAL_CONFIG", t.TempDir())
	t.Setenv("CRUSH_GLOBAL_DATA", t.TempDir())
	c := newGateTestCoordinator(t, true)
	cfg := c.cfg.Config()
	for _, kind := range []catwalk.Type{config.TypeClaudeCode, config.TypeCodexCLI} {
		cfg.Providers.Set(string(kind), config.ProviderConfig{ID: string(kind), Name: string(kind), Type: kind,
			Models: []catwalk.Model{{ID: "opus", Name: "Claude Opus"}, {ID: "gpt-5.6-sol", Name: "GPT-5.6 Sol"}}})
	}
	cfg.Options.Accounts = []config.Account{{ID: "claude-2", Kind: config.TypeClaudeCode}, {ID: "codex-2", Kind: config.TypeCodexCLI}}
	cfg.Options.Fallback = &config.FallbackOrder{On: true, Blocks: blocks}
	cfg.Models[config.SelectedModelTypeLarge] = fallbackModelOf(blocks[0])

	fetch := func(kind catwalk.Type) func(ctx context.Context) ([]cliagent.Limit, error) {
		return func(ctx context.Context) ([]cliagent.Limit, error) {
			key := string(kind) + "/" + cliagent.LimitAccount(ctx)
			cliagent.ReportRefills(ctx, refills[key])
			return []cliagent.Limit{{Name: "5h", Used: usage[key], ResetsAt: time.Now().Add(time.Hour)}}, nil
		}
	}
	t.Cleanup(cliagent.SetLimitFetchersForTest(map[catwalk.Type]func(context.Context) ([]cliagent.Limit, error){
		config.TypeClaudeCode: fetch(config.TypeClaudeCode), config.TypeCodexCLI: fetch(config.TypeCodexCLI),
	}))
	for _, a := range []string{"", "claude-2"} {
		_, err := cliagent.RefreshAccountLimits(t.Context(), config.TypeClaudeCode, a, true)
		require.NoError(t, err)
	}
	for _, a := range []string{"", "codex-2"} {
		_, err := cliagent.RefreshAccountLimits(t.Context(), config.TypeCodexCLI, a, true)
		require.NoError(t, err)
	}
	c.limitRetries.poll = time.Millisecond
	c.fallback.send = func(string) {} // never start a real CLI
	sess, err := c.sessions.Create(t.Context(), "fallback")
	require.NoError(t, err)
	return c, sess.ID
}

func fallbackModelOf(b config.FallbackBlock) config.SelectedModel {
	return config.SelectedModel{Provider: b.Provider, Model: b.Model, Account: b.Account}
}

func useBlock(kind catwalk.Type, account, model string) config.FallbackBlock {
	return config.FallbackBlock{Type: config.FallbackUse, Provider: string(kind), Model: model, Account: account}
}

func large(c *coordinator) config.SelectedModel {
	return c.cfg.Config().Models[config.SelectedModelTypeLarge]
}

func chatLines(t *testing.T, c *coordinator, sessionID string) []string {
	msgs, err := c.messages.List(t.Context(), sessionID)
	require.NoError(t, err)
	var lines []string
	for _, m := range msgs {
		if m.Role == message.System {
			lines = append(lines, m.Content().String())
		}
	}
	return lines
}

var errOut = errors.New("You've hit your session limit · resets 11:59pm")

// The owner's example: Claude 1 runs out, the chat moves to Claude 2 with
// a line saying so, and continues there.
func TestRunningOutSwitchesToTheNextAccountAndContinues(t *testing.T) {
	blocks := []config.FallbackBlock{
		useBlock(config.TypeClaudeCode, "", "opus"),
		useBlock(config.TypeClaudeCode, "claude-2", "opus"),
		{Type: config.FallbackWait, Kind: config.TypeClaudeCode},
		useBlock(config.TypeCodexCLI, "", "gpt-5.6-sol"),
	}
	c, sid := fallbackCoordinator(t, blocks, map[string]float64{"claude-code/": 100}, nil)
	sent := make(chan string, 1)
	c.fallback.send = func(id string) { sent <- id }
	switched := 0
	c.SetModelSwitchHook(func() { switched++ })

	require.True(t, c.fallbackOnOut(t.Context(), sid, errOut))
	require.Equal(t, "claude-2", large(c).Account)
	require.Equal(t, 1, switched)
	lines := chatLines(t, c, sid)
	require.Len(t, lines, 1)
	require.Equal(t, "Switched to Claude 2 · Claude Opus: Claude 1 it ran out.", lines[0])
	// The chat is continued at once.
	select {
	case id := <-sent:
		require.Equal(t, sid, id)
	case <-time.After(5 * time.Second):
		t.Fatal("the chat was not continued")
	}
}

func TestWaitBlockWaitsThenReturnsToTheAccount(t *testing.T) {
	blocks := []config.FallbackBlock{
		useBlock(config.TypeClaudeCode, "", "opus"),
		useBlock(config.TypeClaudeCode, "claude-2", "opus"),
		{Type: config.FallbackWait, Kind: config.TypeClaudeCode},
		useBlock(config.TypeCodexCLI, "", "gpt-5.6-sol"),
	}
	c, sid := fallbackCoordinator(t, blocks, map[string]float64{"claude-code/": 100, "claude-code/claude-2": 100}, nil)
	c.cfg.Config().Models[config.SelectedModelTypeLarge] = fallbackModelOf(blocks[1])
	clock := &fakeClock{now: time.Now()}
	c.limitRetries.now = clock.Now

	require.True(t, c.fallbackOnOut(t.Context(), sid, errOut))
	lines := chatLines(t, c, sid)
	require.Len(t, lines, 1)
	require.True(t, strings.HasPrefix(lines[0], "Claude 2 it ran out. Waiting for Claude 1 to refill at "), lines[0])
	require.Len(t, c.limitRetries.pending, 1)
	require.Equal(t, "claude-2", large(c).Account, "nothing moves before the refill")

	sent := make(chan string, 1)
	c.fallback.send = func(id string) { sent <- id }
	clock.Set(time.Now().Add(25 * time.Hour))
	select {
	case <-sent:
	case <-time.After(5 * time.Second):
		t.Fatal("the chat was not continued after the refill")
	}
	require.Equal(t, "", large(c).Account)
}

func TestRefillFirstSpendsACodexRefill(t *testing.T) {
	first := useBlock(config.TypeCodexCLI, "codex-2", "gpt-5.6-sol")
	first.RefillFirst = true
	blocks := []config.FallbackBlock{first, useBlock(config.TypeClaudeCode, "", "opus")}
	c, sid := fallbackCoordinator(t, blocks, map[string]float64{"codex-cli/codex-2": 100}, map[string]int{"codex-cli/codex-2": 1})
	asked := ""
	t.Cleanup(cliagent.SetConsumeRefillForTest(func(_ context.Context, account string) (string, error) {
		asked = account
		return "reset", nil
	}))
	require.True(t, c.fallbackOnOut(t.Context(), sid, errOut))
	require.Equal(t, "codex-2", asked)
	require.Equal(t, "codex-2", large(c).Account, "the chat stays on the refilled account")
	require.Equal(t, []string{"Used a free refill on Codex 2."}, chatLines(t, c, sid))
}

func TestFailedRefillMovesOn(t *testing.T) {
	first := useBlock(config.TypeCodexCLI, "codex-2", "gpt-5.6-sol")
	first.RefillFirst = true
	blocks := []config.FallbackBlock{first, useBlock(config.TypeClaudeCode, "", "opus")}
	c, sid := fallbackCoordinator(t, blocks, map[string]float64{"codex-cli/codex-2": 100}, map[string]int{"codex-cli/codex-2": 1})
	t.Cleanup(cliagent.SetConsumeRefillForTest(func(context.Context, string) (string, error) { return "noCredit", nil }))
	require.True(t, c.fallbackOnOut(t.Context(), sid, errOut))
	require.Equal(t, config.SelectedModel{Provider: "claude-code", Model: "opus"}, config.SelectedModel{Provider: large(c).Provider, Model: large(c).Model, Account: large(c).Account})
}

func TestComeBackSwitchesBackBeforeATurn(t *testing.T) {
	first := useBlock(config.TypeClaudeCode, "", "opus")
	first.ComeBack = true
	blocks := []config.FallbackBlock{first, useBlock(config.TypeClaudeCode, "claude-2", "opus")}
	c, sid := fallbackCoordinator(t, blocks, map[string]float64{"claude-code/": 20}, nil)
	c.cfg.Config().Models[config.SelectedModelTypeLarge] = fallbackModelOf(blocks[1])
	c.fallbackBeforeTurn(t.Context(), sid)
	require.Equal(t, "", large(c).Account)
	require.Equal(t, []string{"Switched to Claude 1 · Claude Opus by the fallback order."}, chatLines(t, c, sid))
}

func TestAnErrorThatIsNotAUsageLimitLeavesTheChat(t *testing.T) {
	blocks := []config.FallbackBlock{useBlock(config.TypeClaudeCode, "", "opus"), useBlock(config.TypeClaudeCode, "claude-2", "opus")}
	c, sid := fallbackCoordinator(t, blocks, nil, nil)
	require.False(t, c.fallbackOnOut(t.Context(), sid, errors.New("connection reset by peer")))
	require.Equal(t, "", large(c).Account)
	require.Empty(t, chatLines(t, c, sid))
}
