package cliagent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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

// SignOut removes only the chosen OpenCode login entry and leaves the other
// provider's entry intact.
func TestSignOutRemovesOpenCodeEntryWithoutTouchingOther(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("GROK_HOME", "")

	path := filepath.Join(home, "data", "opencode", "auth.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(`{"opencode-go":{"type":"api","key":"go-key"},"opencode":{"type":"api","key":"zen-key"}}`), 0o600))
	require.True(t, SignedIn(config.TypeOpenCodeCLI))
	require.True(t, SignedIn(config.TypeOpenCodeZenCLI))

	require.NoError(t, SignOut(t.Context(), config.TypeOpenCodeCLI))
	require.False(t, SignedIn(config.TypeOpenCodeCLI))
	require.True(t, SignedIn(config.TypeOpenCodeZenCLI), "removing OpenCode Go must keep OpenCode Zen")

	require.NoError(t, SignOut(t.Context(), config.TypeOpenCodeZenCLI))
	require.False(t, SignedIn(config.TypeOpenCodeZenCLI))
}

// SignOut on OpenCode Go also clears the OpenCode Console credential in
// opencode.db and deletes remembered usage limits so it does not stay shown
// as idle after restart.
func TestSignOutOpenCodeGoClearsConsoleAndSavedLimits(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("GROK_HOME", "")
	shareLimitsForTest(t, config.TypeOpenCodeCLI)

	dir := filepath.Join(home, "data", "opencode")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	writeOpenCodeCredential(t, filepath.Join(dir, "opencode.db"), `{"type":"oauth","access":"console-token"}`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.json"), []byte(`{"opencode":{"type":"api","key":"zen-key"}}`), 0o600))
	require.True(t, SignedIn(config.TypeOpenCodeCLI))

	k := limitKey{kind: config.TypeOpenCodeCLI}
	setLimitsAt(k, []Limit{{Name: "5h", Used: 10}}, -1, time.Now())
	require.FileExists(t, savedLimitsPath(k))
	require.NotEmpty(t, Limits(config.TypeOpenCodeCLI, ""))

	require.NoError(t, SignOut(t.Context(), config.TypeOpenCodeCLI))
	require.False(t, SignedIn(config.TypeOpenCodeCLI))
	require.True(t, SignedIn(config.TypeOpenCodeZenCLI))
	require.Empty(t, Limits(config.TypeOpenCodeCLI, ""))
	require.NoFileExists(t, savedLimitsPath(k))
}
