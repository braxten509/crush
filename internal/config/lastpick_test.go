package config

import (
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/stretchr/testify/require"
)

// The last pick is the whole choice the owner picked last, read fresh from
// its own file, so every running engine starts new chats on it. It is never
// a model any chat runs on (owner, 2026-10-24).
func TestLastPickIsReadFreshAcrossStores(t *testing.T) {
	t.Setenv("CRUSH_GLOBAL_DATA", t.TempDir())

	_, ok := ReadLastPick()
	require.False(t, ok, "nothing picked yet")

	cfg := &Config{
		Providers: csync.NewMapFrom(map[string]ProviderConfig{
			"claude-code": {ID: "claude-code", Models: []catwalk.Model{{ID: "opus"}}},
			"codex-cli":   {ID: "codex-cli", Models: []catwalk.Model{{ID: "gpt-6-sol"}}},
		}),
		Models: map[SelectedModelType]SelectedModel{
			SelectedModelTypeLarge: {Provider: "claude-code", Model: "opus"},
		},
	}
	// With no pick yet, the first new chat seeds it from the starting model.
	seeded, ok := NewChatModel(cfg)
	require.True(t, ok)
	require.Equal(t, SelectedModel{Provider: "claude-code", Model: "opus"}, seeded)

	picked := SelectedModel{Provider: "codex-cli", Model: "gpt-6-sol", Account: "codex-2", ReasoningEffort: "max", ServiceTier: "fast"}
	require.NoError(t, WriteLastPick(picked))

	// Another engine reads the file, not a copy it keeps.
	read, ok := ReadLastPick()
	require.True(t, ok)
	require.Equal(t, picked, read, "the whole choice comes back")
	fresh, ok := NewChatModel(cfg)
	require.True(t, ok)
	require.Equal(t, picked, fresh)

	// A folder whose config doesn't offer the pick starts on its own
	// starting model, and the pick stays for the others.
	other := &Config{
		Providers: csync.NewMapFrom(map[string]ProviderConfig{"claude-code": {ID: "claude-code", Models: []catwalk.Model{{ID: "opus"}}}}),
		Models:    map[SelectedModelType]SelectedModel{SelectedModelTypeLarge: {Provider: "claude-code", Model: "opus"}},
	}
	start, ok := NewChatModel(other)
	require.True(t, ok)
	require.Equal(t, SelectedModel{Provider: "claude-code", Model: "opus"}, start)
	read, _ = ReadLastPick()
	require.Equal(t, picked, read)

	require.Equal(t, SelectedModel{Provider: "claude-code", Model: "opus"}, cfg.Models[SelectedModelTypeLarge], "a pick writes no model")
}
