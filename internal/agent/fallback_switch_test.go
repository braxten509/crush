package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/fallback"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

// fallbackCoordinator is a coordinator whose chats run on Claude and Codex
// accounts under the given order, reading usage from fake fetchers: usage
// by "kind/account" (read at each fetch, so a test can change it; below 0
// the reading fails). No real login is read, so accounts go by number, and
// the hidden request that tests an account fails unless a test sets it.
func fallbackCoordinator(t *testing.T, blocks []config.FallbackBlock, usage map[string]float64, refills map[string]int) (*coordinator, string) {
	t.Helper()
	t.Setenv("CRUSH_GLOBAL_CONFIG", t.TempDir())
	t.Setenv("CRUSH_GLOBAL_DATA", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	c := newGateTestCoordinator(t, true)
	// The coordinator builds its agent's tools in the background; the config
	// changes below wait for that, or they race its reads of the models.
	_ = c.readyWg.Wait()
	cfg := c.cfg.Config()
	for _, kind := range []catwalk.Type{config.TypeClaudeCode, config.TypeCodexCLI} {
		cfg.Providers.Set(string(kind), config.ProviderConfig{ID: string(kind), Name: string(kind), Type: kind,
			Models: []catwalk.Model{{ID: "opus", Name: "Claude Opus"}, {ID: "gpt-5.6-sol", Name: "GPT-5.6 Sol"}}})
	}
	cfg.Options.Accounts = []config.Account{{ID: "claude-2", Kind: config.TypeClaudeCode}, {ID: "codex-2", Kind: config.TypeCodexCLI}}
	// The added accounts are signed in.
	for id, login := range map[string]string{"claude-2": ".credentials.json", "codex-2": "auth.json"} {
		require.NoError(t, os.MkdirAll(config.AccountDir(id), 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(config.AccountDir(id), login), []byte("{}"), 0o600))
	}
	cfg.Options.Fallback = &config.FallbackOrder{On: true, Blocks: blocks}
	cfg.Models[config.SelectedModelTypeLarge] = fallbackModelOf(blocks[0])

	fetch := func(kind catwalk.Type) func(ctx context.Context) ([]cliagent.Limit, error) {
		return func(ctx context.Context) ([]cliagent.Limit, error) {
			key := string(kind) + "/" + cliagent.LimitAccount(ctx)
			if usage[key] < 0 {
				return nil, errors.New("claude login expired")
			}
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
	c.fallback.try = func(context.Context, catwalk.Type, string, string) error { return errors.New("no real CLI in tests") }
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
	var seen string // the agent's account when the apps are told
	c.SetModelSwitchHook(func() { switched++; seen = c.Model().ModelCfg.Account })

	require.True(t, c.fallbackOnOut(t.Context(), sid, errOut))
	require.Equal(t, "claude-2", large(c).Account)
	require.Equal(t, 1, switched)
	require.Equal(t, "claude-2", seen, "the agent runs on the new account before the apps read it")
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
	usage := map[string]float64{"claude-code/": 100, "claude-code/claude-2": 100}
	c, sid := fallbackCoordinator(t, blocks, usage, nil)
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
	usage["claude-code/"] = 0 // refilled
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

// Before a chat moves to an account, the account is checked quietly (owner,
// 2026-10-21): Claude 2's last reading had room, but it is used up now, so
// the chat goes on to Codex, and its one line names only where it went.
func TestASwitchSkipsAnAccountWithNoRoomLeft(t *testing.T) {
	blocks := []config.FallbackBlock{
		useBlock(config.TypeClaudeCode, "", "opus"),
		useBlock(config.TypeClaudeCode, "claude-2", "opus"),
		useBlock(config.TypeCodexCLI, "", "gpt-5.6-sol"),
	}
	usage := map[string]float64{"claude-code/": 100}
	c, sid := fallbackCoordinator(t, blocks, usage, nil)
	usage["claude-code/claude-2"] = 100 // used up since its last reading

	require.True(t, c.fallbackOnOut(t.Context(), sid, errOut))
	require.Equal(t, config.SelectedModel{Provider: "codex-cli", Model: "gpt-5.6-sol", ChosenProvider: "claude-code", ChosenModel: "opus"}, large(c))
	require.Equal(t, []string{"Switched to Codex 1 · GPT-5.6 Sol: Claude 1 it ran out."}, chatLines(t, c, sid))
}

// When an account's usage can't be read, one hidden request tests it
// ("try it silently not in the chat"): the chat moves there only if it
// answers, and nothing about the test shows in the chat.
func TestASwitchTriesAnAccountWhoseUsageCantBeRead(t *testing.T) {
	blocks := []config.FallbackBlock{
		useBlock(config.TypeClaudeCode, "", "opus"),
		useBlock(config.TypeClaudeCode, "claude-2", "opus"),
		useBlock(config.TypeCodexCLI, "", "gpt-5.6-sol"),
	}
	for _, tc := range []struct {
		name   string
		answer error
		want   config.SelectedModel
		line   string
	}{
		{"it answers", nil, config.SelectedModel{Provider: "claude-code", Model: "opus", Account: "claude-2"}, "Switched to Claude 2 · Claude Opus: Claude 1 it ran out."},
		{"it is out too", errors.New("claude: You've hit your limit · resets 11:59pm"), config.SelectedModel{Provider: "codex-cli", Model: "gpt-5.6-sol", ChosenProvider: "claude-code", ChosenModel: "opus"}, "Switched to Codex 1 · GPT-5.6 Sol: Claude 1 it ran out."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			usage := map[string]float64{"claude-code/": 100}
			c, sid := fallbackCoordinator(t, blocks, usage, nil)
			usage["claude-code/claude-2"] = -1 // its reading fails now
			var tried []string
			c.fallback.try = func(_ context.Context, kind catwalk.Type, account, model string) error {
				tried = append(tried, string(kind)+"/"+account+" "+model)
				return tc.answer
			}

			require.True(t, c.fallbackOnOut(t.Context(), sid, errOut))
			require.Equal(t, []string{"claude-code/claude-2 opus"}, tried)
			require.Equal(t, tc.want, large(c))
			require.Equal(t, []string{tc.line}, chatLines(t, c, sid))
		})
	}
}

// The owner's example (2026-10-21): each picked model has its own list.
// A chat picked on GPT follows GPT's list (on to Claude 2), where every
// other model's list has nothing after GPT; the picked model stays its
// start after the move. A sub-agent started on GPT follows the same list.
func TestEachPickedModelFollowsItsOwnList(t *testing.T) {
	blocks := []config.FallbackBlock{
		useBlock(config.TypeClaudeCode, "", "opus"),
		useBlock(config.TypeCodexCLI, "", "gpt-5.6-sol"),
	}
	c, sid := fallbackCoordinator(t, blocks, map[string]float64{"codex-cli/": 100}, nil)
	cfg := c.cfg.Config()
	cfg.Options.Fallback.Lists = []config.FallbackList{{Provider: string(config.TypeCodexCLI), Model: "gpt-5.6-sol", Blocks: []config.FallbackBlock{
		useBlock(config.TypeCodexCLI, "", "gpt-5.6-sol"),
		useBlock(config.TypeClaudeCode, "claude-2", "opus"),
	}}}
	gpt := fallbackModelOf(blocks[1])
	cfg.Models[config.SelectedModelTypeLarge] = gpt

	require.True(t, c.fallbackOnOut(t.Context(), sid, errOut))
	got := large(c)
	require.Equal(t, [3]string{"claude-code", "opus", "claude-2"}, [3]string{got.Provider, got.Model, got.Account})
	require.Equal(t, [2]string{"codex-cli", "gpt-5.6-sol"}, [2]string{got.ChosenProvider, got.ChosenModel}, "the picked model stays the start")

	hub := &taskHub{c: c, dir: t.TempDir()}
	task := &Task{ID: "t1", SessionID: sid, ChildID: sid}
	next, ok := hub.subAgentFallback(t.Context(), task, gpt, gpt, errOut)
	require.True(t, ok, "a sub-agent started on GPT follows GPT's list")
	require.Equal(t, [2]string{"opus", "claude-2"}, [2]string{next.model.ModelCfg.Model, next.model.ModelCfg.Account})
	require.Equal(t, "opus", task.Model)
}

// opusOrder is the owner's real order (2026-10-22): a list for Claude Opus
// with three Claude accounts at high effort, the first two marked "come
// back", and an empty list for every other model. A chat starts on Opus on
// the owner's own login, the list's second block.
func opusOrder(t *testing.T, usage map[string]float64) (*coordinator, string, *fakeClock) {
	t.Helper()
	list := []config.FallbackBlock{
		useBlock(config.TypeClaudeCode, "claude-2", "opus"),
		useBlock(config.TypeClaudeCode, "", "opus"),
		useBlock(config.TypeClaudeCode, "claude-3", "opus"),
	}
	for i := range list {
		list[i].Effort = "high"
	}
	list[0].ComeBack, list[1].ComeBack = true, true
	c, sid := fallbackCoordinator(t, list[1:2], usage, nil)
	cfg := c.cfg.Config()
	cfg.Options.Accounts = append(cfg.Options.Accounts, config.Account{ID: "claude-3", Kind: config.TypeClaudeCode})
	require.NoError(t, os.MkdirAll(config.AccountDir("claude-3"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(config.AccountDir("claude-3"), ".credentials.json"), []byte("{}"), 0o600))
	_, err := cliagent.RefreshAccountLimits(t.Context(), config.TypeClaudeCode, "claude-3", true)
	require.NoError(t, err)
	cfg.Options.Fallback = &config.FallbackOrder{On: true, Lists: []config.FallbackList{{Provider: string(config.TypeClaudeCode), Model: "opus", Blocks: list}}}
	clock := &fakeClock{now: time.Now()}
	c.fallback.now = clock.Now
	c.limitRetries.now = clock.Now
	return c, sid, clock
}

// where is the chat's account and effort, and the model whose list it
// follows.
func where(c *coordinator) [3]string {
	m := large(c)
	provider, model := fallback.Chosen(m)
	return [3]string{m.Account, m.ReasoningEffort, provider + "/" + model}
}

// The owner's order walked through: back to account 1 before the first
// turn, on to 2 and then 3 as each runs out, then back to 2 once it
// refills while 1 is still out, then back to 1.
func TestTheOwnersOpusOrderStepByStep(t *testing.T) {
	usage := map[string]float64{}
	c, sid, clock := opusOrder(t, usage)
	start := clock.Now()

	c.fallbackBeforeTurn(t.Context(), sid)
	require.Equal(t, [3]string{"claude-2", "high", "claude-code/opus"}, where(c), "account 1 has room: the chat comes back to it")

	usage["claude-code/claude-2"] = 100
	require.True(t, c.fallbackOnOut(t.Context(), sid, errors.New("You've hit your limit · resets in 2h")))
	require.Equal(t, [3]string{"", "high", "claude-code/opus"}, where(c))

	usage["claude-code/"] = 100
	require.True(t, c.fallbackOnOut(t.Context(), sid, errors.New("You've hit your limit · resets in 3h")))
	require.Equal(t, [3]string{"claude-3", "high", "claude-code/opus"}, where(c))

	// Account 2 refilled (3h on), account 1 is still out: back to 2.
	usage["claude-code/"] = 0
	clock.Set(start.Add(3*time.Hour + time.Minute))
	c.fallbackBeforeTurn(t.Context(), sid)
	require.Equal(t, [3]string{"", "high", "claude-code/opus"}, where(c))

	// Account 1 refilled too, as the between-turns reading shows: back to 1.
	usage["claude-code/claude-2"] = 0
	_, err := cliagent.RefreshAccountLimits(t.Context(), config.TypeClaudeCode, "claude-2", true)
	require.NoError(t, err)
	c.fallbackBeforeTurn(t.Context(), sid)
	require.Equal(t, [3]string{"claude-2", "high", "claude-code/opus"}, where(c))

	require.Equal(t, []string{
		"Switched to Claude 2 · Claude Opus by the fallback order.",
		"Switched to Claude 1 · Claude Opus: Claude 2 it ran out.",
		"Switched to Claude 3 · Claude Opus: Claude 1 it ran out.",
		"Switched to Claude 1 · Claude Opus by the fallback order.",
		"Switched to Claude 2 · Claude Opus by the fallback order.",
	}, chatLines(t, c, sid))
}
