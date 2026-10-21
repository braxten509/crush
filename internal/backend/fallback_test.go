package backend

import (
	"testing"

	"github.com/charmbracelet/crush/internal/proto"
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
