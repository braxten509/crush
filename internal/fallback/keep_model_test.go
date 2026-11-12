package fallback

import (
	"testing"
	"time"

	"charm.land/catwalk/v3/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

// A keep-model block moves a chat on its tool to its sign-in with the chat's
// own model and effort, and a chat on another tool passes it by.
func TestKeepModelBlockKeepsTheChatsModel(t *testing.T) {
	kinds := func(provider string) catwalk.Type { return catwalk.Type(provider) }
	keep := func(kind catwalk.Type, id string) config.FallbackBlock {
		return config.FallbackBlock{Type: config.FallbackUse, KeepModel: true, Kind: kind, Account: id}
	}
	saved := config.FallbackOrder{On: true, Blocks: []config.FallbackBlock{
		{Type: config.FallbackUse, Provider: "claude-code", Model: "opus"},
		keep(config.TypeCodexCLI, "x2"),
		keep(config.TypeClaudeCode, "c2"),
		{Type: config.FallbackUse, Provider: "codex-cli", Model: "gpt"},
	}}
	chat := config.SelectedModel{Provider: "claude-code", Model: "sonnet", ReasoningEffort: "high"}
	order := Keep(saved, kinds, chat, chat.Provider, chat.Model)
	require.True(t, saved.Blocks[2].KeepModel, "the saved order is left as it is")

	at := Position(order, kinds, chat)
	require.Equal(t, 0, at)
	clock := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	full := Reading{Known: true, Used: 100, OpenAt: clock.Add(time.Hour)}
	read := func(a Account) Reading {
		if a == (Account{Kind: config.TypeClaudeCode}) {
			return full
		}
		return Reading{}
	}
	// The Codex keep-model block is passed by; the Claude one takes the chat.
	act := OnOut(order, kinds, at, read, clock, nil)
	require.Equal(t, Action{Kind: Switch, Block: 2}, act)
	require.Equal(t, config.SelectedModel{Provider: "claude-code", Model: "sonnet", ReasoningEffort: "high", Account: "c2"}, Model(order.Blocks[act.Block]))

	// Once there, the chat is at that block, not about to move to it.
	moved := chat
	moved.Account = "c2"
	require.Equal(t, 2, Position(Keep(saved, kinds, moved, chat.Provider, chat.Model), kinds, moved))
}
