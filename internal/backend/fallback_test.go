package backend

import (
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
