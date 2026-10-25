package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The last pick is the whole choice the owner picked last, read fresh from
// its own file, so every running engine starts new chats on it. It is never
// a model any chat runs on (owner, 2026-10-24).
func TestLastPickIsReadFreshAcrossStores(t *testing.T) {
	t.Setenv("CRUSH_GLOBAL_DATA", t.TempDir())

	_, ok := ReadLastPick()
	require.False(t, ok, "nothing picked yet")

	// With no pick yet, the first new chat seeds it from the starting model.
	cfg := &Config{Models: map[SelectedModelType]SelectedModel{
		SelectedModelTypeLarge: {Provider: "claude-code", Model: "opus"},
	}}
	seeded, ok := NewChatModel(cfg)
	require.True(t, ok)
	require.Equal(t, SelectedModel{Provider: "claude-code", Model: "opus"}, seeded)

	picked := SelectedModel{Provider: "codex-cli", Model: "gpt-6-sol", Account: "codex-2", ReasoningEffort: "max", ServiceTier: "fast"}
	require.NoError(t, WriteLastPick(picked))

	// Another engine reads the file, not a copy it keeps.
	read, ok := ReadLastPick()
	require.True(t, ok)
	require.Equal(t, picked, read, "the whole choice comes back")
	fresh, ok := NewChatModel(&Config{})
	require.True(t, ok)
	require.Equal(t, picked, fresh)

	require.Equal(t, SelectedModel{Provider: "claude-code", Model: "opus"}, cfg.Models[SelectedModelTypeLarge], "a pick writes no model")
}
