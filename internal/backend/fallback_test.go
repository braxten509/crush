package backend

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An API provider's second account is a provider entry of its own: the same
// type, endpoint and models, a name that says which account it is, and no
// key yet (owner, 2026-10-24: every provider in the picker can have more
// than one account).
func TestAddProviderAccountCopiesTheProviderWithoutItsKey(t *testing.T) {
	b, ws, _ := newPublishingWorkspace(t)
	require.NoError(t, b.SetConfigField(ws.ID, config.ScopeGlobal, "providers.mine", config.ProviderConfig{
		ID:      "mine",
		Name:    "Mine",
		Type:    "openai",
		BaseURL: "https://example.test/v1",
		APIKey:  "sk-test",
		Models:  []catwalk.Model{{ID: "m1", Name: "M1"}},
	}))

	login, err := b.AddAccount(ws.ID, "mine")
	require.NoError(t, err)
	require.Equal(t, "mine-2", login.Account.ID)
	require.Empty(t, login.LoginArgv, "an API account signs in with its key, not a terminal")
	require.Equal(t, "Mine (2)", login.Account.Name)

	original, ok := ws.Cfg.Config().Providers.Get("mine")
	require.True(t, ok)
	copied, ok := ws.Cfg.Config().Providers.Get("mine-2")
	require.True(t, ok)
	require.Equal(t, original.Type, copied.Type)
	require.Equal(t, original.BaseURL, copied.BaseURL)
	require.Empty(t, copied.APIKey, "the copy waits for its own key")
	require.Equal(t, original.Models, copied.Models)

	// A CLI whose login is a single one cannot be copied.
	_, err = b.AddAccount(ws.ID, "agy-cli")
	require.Error(t, err)
}

// The engine tells the desktop which CLIs hold their own login, so Settings →
// Accounts can say Signed in without the page reading credential files
// itself (owner, 2026-10-24). Tests never touch AGY's real keyring.
func TestAuthStateListsTheCLILogins(t *testing.T) {
	b, ws, _ := newPublishingWorkspace(t)
	home := t.TempDir() // after the workspace: it isolates HOME itself
	t.Setenv("HOME", home)
	t.Setenv("GROK_HOME", "")

	got, err := b.AuthState(ws.ID)
	require.NoError(t, err)
	require.False(t, got.Providers["grok-cli"])
	require.False(t, got.Providers["agy-cli"])

	require.NoError(t, os.MkdirAll(filepath.Join(home, ".grok"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".grok", "auth.json"), []byte(`{"access_token":"x"}`), 0o600))
	got, err = b.AuthState(ws.ID)
	require.NoError(t, err)
	require.True(t, got.Providers["grok-cli"])
}

// A tool's own login is renamed by the tool's name in place of an account
// ID: the name is kept in options.own_account_names, and an empty name goes
// back to the default (no email here, so its number).
func TestRenameOwnLogin(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // no ~/.claude.json, so no email
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	b, ws, _ := newPublishingWorkspace(t)

	got, err := b.RenameAccount(ws.ID, "claude-code", "  Work  ")
	require.NoError(t, err)
	require.Equal(t, proto.AccountInfo{Kind: "claude-code", Name: "Work", Label: "Work", SignedIn: true}, got.Accounts[0])
	require.Equal(t, map[string]string{"claude-code": "Work"}, ws.Cfg.Config().Options.OwnAccountNames)

	got, err = b.RenameAccount(ws.ID, "claude-code", "")
	require.NoError(t, err)
	require.Equal(t, "Claude 1", got.Accounts[0].Name)
	require.Empty(t, ws.Cfg.Config().Options.OwnAccountNames)

	_, err = b.RenameAccount(ws.ID, "claude-9", "x")
	require.ErrorIs(t, err, ErrAccountNotFound)
}

// An account edit answers with what it wrote, even when its own reload was
// skipped because another reload was running (the folder's config watcher
// reloads after every write; config.autoReload): it waits for a reload of
// its own (2026-10-21, TestRenameOwnLogin failed in the full run).
func TestAccountEditAnswersWithWhatItWrote(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	b, ws, _ := newPublishingWorkspace(t)
	release := ws.Cfg.HoldReloadsForTest()
	t.Cleanup(release)
	done := make(chan proto.FallbackSettings, 1)
	go func() {
		got, err := b.RenameAccount(ws.ID, "claude-code", "Work")
		assert.NoError(t, err)
		done <- got
	}()
	select {
	case got := <-done:
		t.Fatalf("answered before the config reloaded: %q", got.Accounts[0].Name)
	case <-time.After(300 * time.Millisecond):
	}
	release()
	select {
	case got := <-done:
		require.Equal(t, "Work", got.Accounts[0].Name)
	case <-time.After(10 * time.Second):
		t.Fatal("no answer after the reload")
	}
}
