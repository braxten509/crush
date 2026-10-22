package fallback

import (
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

var now = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

func kindOf(provider string) catwalk.Type { return catwalk.Type(provider) }

var (
	claude1 = Account{Kind: config.TypeClaudeCode}
	claude2 = Account{Kind: config.TypeClaudeCode, ID: "c2"}
	codex1  = Account{Kind: config.TypeCodexCLI}
)

func use(acct Account, model string) config.FallbackBlock {
	return config.FallbackBlock{Type: config.FallbackUse, Provider: string(acct.Kind), Model: model, Account: acct.ID}
}

func wait(acct Account) config.FallbackBlock {
	return config.FallbackBlock{Type: config.FallbackWait, Kind: acct.Kind, Account: acct.ID}
}

// readings: accounts not listed have room and no refills.
func readings(m map[Account]Reading) Readings {
	return func(a Account) Reading { return m[a] }
}

func out(at time.Time) Reading { return Reading{Known: true, Used: 100, OpenAt: at} }

// The owner's example: Claude 1 → Claude 2 → wait for Claude 1 → Codex.
func example() config.FallbackOrder {
	return config.FallbackOrder{On: true, Blocks: []config.FallbackBlock{
		use(claude1, "opus"), use(claude2, "opus"), wait(claude1), use(codex1, "gpt-5.6-sol"),
	}}
}

func TestPositionFindsTheChatsBlock(t *testing.T) {
	order := example()
	require.Equal(t, 1, Position(order, kindOf, config.SelectedModel{Provider: "claude-code", Model: "opus", Account: "c2"}))
	// Another model on the same account still counts as that block.
	require.Equal(t, 0, Position(order, kindOf, config.SelectedModel{Provider: "claude-code", Model: "sonnet"}))
	require.Equal(t, -1, Position(order, kindOf, config.SelectedModel{Provider: "grok-cli", Model: "grok"}))
	order.On = false
	require.Equal(t, -1, Position(order, kindOf, config.SelectedModel{Provider: "claude-code", Model: "opus"}))
}

func TestOutMovesToTheNextBlockWithRoom(t *testing.T) {
	act := OnOut(example(), kindOf, 0, readings(map[Account]Reading{claude1: out(now.Add(time.Hour))}), now, nil)
	require.Equal(t, Action{Kind: Switch, Block: 1}, act)
}

func TestWaitBlockWaitsForItsAccount(t *testing.T) {
	r := readings(map[Account]Reading{claude1: out(now.Add(time.Hour)), claude2: out(now.Add(2 * time.Hour))})
	act := OnOut(example(), kindOf, 1, r, now, nil)
	require.Equal(t, Action{Kind: Wait, Block: 0, Until: now.Add(time.Hour)}, act)
}

func TestWaitBlockSkipsAWaitLongerThanItsMax(t *testing.T) {
	order := example()
	order.Blocks[2].MaxWaitMinutes = 30
	r := readings(map[Account]Reading{claude1: out(now.Add(time.Hour)), claude2: out(now.Add(2 * time.Hour))})
	require.Equal(t, Action{Kind: Switch, Block: 3}, OnOut(order, kindOf, 1, r, now, nil))
	order.Blocks[2].MaxWaitMinutes = 90
	require.Equal(t, Kind(Wait), OnOut(order, kindOf, 1, r, now, nil).Kind)
}

func TestWaitBlockMeanwhileUsesTheNextBlockAndComesBack(t *testing.T) {
	order := example()
	order.Blocks[2].Meanwhile = config.FallbackMeanwhileNext
	r := map[Account]Reading{claude1: out(now.Add(time.Hour)), claude2: out(now.Add(2 * time.Hour))}
	act := OnOut(order, kindOf, 1, readings(r), now, nil)
	require.Equal(t, Action{Kind: Switch, Block: 3, Return: &Return{Block: 0, At: now.Add(time.Hour)}}, act)

	// On Codex, before the refill time: stay. After it, with room: back.
	require.Equal(t, Kind(Stay), BeforeTurn(order, kindOf, 3, act.Return, readings(r), now.Add(30*time.Minute)).Kind)
	r[claude1] = Reading{Known: true, Used: 0}
	require.Equal(t, Action{Kind: Switch, Block: 0}, BeforeTurn(order, kindOf, 3, act.Return, readings(r), now.Add(61*time.Minute)))

	// Nothing after the wait block has room: wait after all.
	r[claude1] = out(now.Add(time.Hour))
	r[codex1] = out(now.Add(5 * time.Hour))
	require.Equal(t, Action{Kind: Wait, Block: 0, Until: now.Add(time.Hour)}, OnOut(order, kindOf, 1, readings(r), now, nil))
}

func TestWaitBlockWithRoomAlreadyGoesStraightBack(t *testing.T) {
	r := readings(map[Account]Reading{claude2: out(now.Add(time.Hour))}) // Claude 1 has room again
	require.Equal(t, Action{Kind: Switch, Block: 0}, OnOut(example(), kindOf, 1, r, now, nil))
}

func TestWaitWithNoKnownRefillTimeIsSkipped(t *testing.T) {
	r := readings(map[Account]Reading{claude1: {Known: true, Used: 100}, claude2: out(now.Add(time.Hour))})
	require.Equal(t, Action{Kind: Switch, Block: 3}, OnOut(example(), kindOf, 1, r, now, nil))
}

func TestRefillFirstSpendsAFreeRefillBeforeMovingOn(t *testing.T) {
	order := config.FallbackOrder{On: true, Blocks: []config.FallbackBlock{use(codex1, "gpt-5.6-sol"), use(claude1, "opus")}}
	order.Blocks[0].RefillFirst = true
	r := map[Account]Reading{codex1: {Known: true, Used: 100, Refills: 2}}
	require.Equal(t, Action{Kind: Refill, Account: codex1, Block: 0}, OnOut(order, kindOf, 0, readings(r), now, nil))
	// A refill that failed is not tried again: move on.
	require.Equal(t, Action{Kind: Switch, Block: 1}, OnOut(order, kindOf, 0, readings(r), now, map[Account]bool{codex1: true}))
	// Without the option, a refill is never spent.
	order.Blocks[0].RefillFirst = false
	require.Equal(t, Action{Kind: Switch, Block: 1}, OnOut(order, kindOf, 0, readings(r), now, nil))
}

func TestRefillBlockSpendsARefillOnlyWhenOneIsThere(t *testing.T) {
	refill := config.FallbackBlock{Type: config.FallbackRefill, Kind: config.TypeCodexCLI}
	order := config.FallbackOrder{On: true, Blocks: []config.FallbackBlock{use(codex1, "gpt-5.6-sol"), use(claude1, "opus"), refill, use(claude2, "opus")}}
	r := map[Account]Reading{codex1: {Known: true, Used: 100, Refills: 1}, claude1: out(now.Add(time.Hour))}
	require.Equal(t, Action{Kind: Refill, Account: codex1, Block: 0}, OnOut(order, kindOf, 1, readings(r), now, nil))
	r[codex1] = Reading{Known: true, Used: 100}
	require.Equal(t, Action{Kind: Switch, Block: 3}, OnOut(order, kindOf, 1, readings(r), now, nil))
}

func TestMoveAtPercentMovesOnEarly(t *testing.T) {
	order := example()
	order.Blocks[0].MoveAtPercent = 95
	r := map[Account]Reading{claude1: {Known: true, Used: 96}}
	require.Equal(t, Action{Kind: Switch, Block: 1}, BeforeTurn(order, kindOf, 0, nil, readings(r), now))
	r[claude1] = Reading{Known: true, Used: 90}
	require.Equal(t, Kind(Stay), BeforeTurn(order, kindOf, 0, nil, readings(r), now).Kind)
	// The next block's own limit counts too: Claude 2 at 96% of a 95% block is full.
	order.Blocks[1].MoveAtPercent = 95
	r = map[Account]Reading{claude1: out(now.Add(time.Hour)), claude2: {Known: true, Used: 96}}
	require.Equal(t, Action{Kind: Wait, Block: 0, Until: now.Add(time.Hour)}, OnOut(order, kindOf, 0, readings(r), now, nil))
}

func TestComeBackReturnsToAnEarlierBlockWithRoom(t *testing.T) {
	order := example()
	r := map[Account]Reading{claude1: {Known: true, Used: 10}}
	// Without "come back" the chat stays on Claude 2.
	require.Equal(t, Kind(Stay), BeforeTurn(order, kindOf, 1, nil, readings(r), now).Kind)
	order.Blocks[0].ComeBack = true
	require.Equal(t, Action{Kind: Switch, Block: 0}, BeforeTurn(order, kindOf, 1, nil, readings(r), now))
	r[claude1] = out(now.Add(time.Hour))
	require.Equal(t, Kind(Stay), BeforeTurn(order, kindOf, 1, nil, readings(r), now).Kind)
}

func TestTheLastBlockRunningOutStays(t *testing.T) {
	r := readings(map[Account]Reading{codex1: out(now.Add(time.Hour))})
	require.Equal(t, Kind(Stay), OnOut(example(), kindOf, 3, r, now, nil).Kind)
}

// With every account out, the chat waits for the "come back" account that
// refills first, unless its own account refills sooner (owner, 2026-10-22).
func TestAllOutWaitsForTheSoonestComeBackRefill(t *testing.T) {
	claude3 := Account{Kind: config.TypeClaudeCode, ID: "c3"}
	order := config.FallbackOrder{On: true, Blocks: []config.FallbackBlock{use(claude2, "opus"), use(claude1, "opus"), use(claude3, "opus")}}
	order.Blocks[0].ComeBack, order.Blocks[1].ComeBack = true, true
	r := map[Account]Reading{claude2: out(now.Add(3 * time.Hour)), claude1: out(now.Add(time.Hour)), claude3: out(now.Add(5 * time.Hour))}
	require.Equal(t, Action{Kind: Wait, Block: 1, Until: now.Add(time.Hour)}, OnOut(order, kindOf, 2, readings(r), now, nil))
	// Its own account refills sooner: stay, and the chat goes on when it does.
	r[claude3] = out(now.Add(30 * time.Minute))
	require.Equal(t, Kind(Stay), OnOut(order, kindOf, 2, readings(r), now, nil).Kind)
	// Without "come back", nothing earlier is waited for.
	r[claude3] = out(now.Add(5 * time.Hour))
	order.Blocks[0].ComeBack, order.Blocks[1].ComeBack = false, false
	require.Equal(t, Kind(Stay), OnOut(order, kindOf, 2, readings(r), now, nil).Kind)
}
