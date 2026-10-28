package cliagent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

// The CLIs that sign in themselves answer from the files they keep their
// login in; the tools whose sign-in is an account folder or an API key are
// not listed (owner, 2026-10-24: Settings → Accounts shows the four CLIs'
// state).
func TestSignedInReadsEachCLIsOwnLogin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GROK_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	write := func(path, text string) {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte(text), 0o600))
	}

	require.False(t, SignedIn(config.TypeGrokCLI), "no auth.json yet")
	write(filepath.Join(home, ".grok", "auth.json"), `{"access_token":"x"}`)
	require.True(t, SignedIn(config.TypeGrokCLI))

	// OpenCode keeps both sign-ins in one auth.json under its data folder.
	require.False(t, SignedIn(config.TypeOpenCodeCLI))
	require.False(t, SignedIn(config.TypeOpenCodeZenCLI))
	write(filepath.Join(home, ".local", "share", "opencode", "auth.json"), `{"opencode-go":{"type":"api","key":"x"},"opencode":{"type":"api","key":"y"}}`)
	require.True(t, SignedIn(config.TypeOpenCodeCLI))
	require.True(t, SignedIn(config.TypeOpenCodeZenCLI))

	// XDG_DATA_HOME moves that folder.
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "xdg"))
	require.False(t, SignedIn(config.TypeOpenCodeCLI))
	require.False(t, SignedIn(config.TypeOpenCodeZenCLI))
	write(filepath.Join(home, "xdg", "opencode", "auth.json"), `{"opencode-go":{"type":"api","key":"x"}}`)
	require.True(t, SignedIn(config.TypeOpenCodeCLI))
	require.False(t, SignedIn(config.TypeOpenCodeZenCLI))

	// AGY's keyring is never read by a test.
	require.False(t, SignedIn(config.TypeAGYCLI))

	require.False(t, SignedIn(config.TypeClaudeCode), "account folders are listed by the fallback settings")
	require.False(t, SignedIn(config.TypeCodexCLI))
}

// Antigravity's row shows the email it signed in with (owner, 2026-10-28):
// the active account in ~/.gemini/google_accounts.json. OpenCode keeps only
// a key, so it and every other kind have none.
func TestLoginEmailReadsAntigravitysActiveAccount(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.Empty(t, LoginEmail(config.TypeAGYCLI), "no accounts file yet")

	path := filepath.Join(home, ".gemini", "google_accounts.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(`{"active":" me@example.com ","old":["old@example.com"]}`), 0o600))
	require.Equal(t, "me@example.com", LoginEmail(config.TypeAGYCLI))
	require.Empty(t, LoginEmail(config.TypeOpenCodeCLI))
}
