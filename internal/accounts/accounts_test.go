package accounts

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

// An account folder links everything of the tool's own folder but its
// login, gains Claude's MCP servers, and removing it keeps what it linked.
func TestAccountFolderSharesAllButTheLogin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CRUSH_GLOBAL_DATA", filepath.Join(home, "data"))
	claude := filepath.Join(home, ".claude")
	require.NoError(t, os.MkdirAll(filepath.Join(claude, "projects", "-p"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(claude, "projects", "-p", "s1.jsonl"), []byte("{}\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(claude, "settings.json"), []byte(`{"a":1}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(claude, ".credentials.json"), []byte("secret-1"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(claude, ".oauth_refresh.lock"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{"oauthAccount":{"a":1},"mcpServers":{"docs":{"command":"x"}}}`), 0o600))

	dir, err := Prepare(config.TypeClaudeCode, "claude-2")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, "data", "accounts", "claude-2"), dir)
	// Settings and saved sessions are account 1's own files.
	got, err := os.ReadFile(filepath.Join(dir, "projects", "-p", "s1.jsonl"))
	require.NoError(t, err)
	require.Equal(t, "{}\n", string(got))
	link, err := os.Readlink(filepath.Join(dir, "settings.json"))
	require.NoError(t, err)
	require.Equal(t, filepath.Join(claude, "settings.json"), link)
	// The login and lock files are never shared.
	for _, name := range []string{".credentials.json", ".oauth_refresh.lock"} {
		_, err := os.Lstat(filepath.Join(dir, name))
		require.True(t, os.IsNotExist(err), name)
	}
	require.False(t, SignedIn(config.TypeClaudeCode, "claude-2"))
	// The MCP servers came along; the account's own login did not.
	settings, err := os.ReadFile(filepath.Join(dir, ".claude.json"))
	require.NoError(t, err)
	require.JSONEq(t, `{"mcpServers":{"docs":{"command":"x"}}}`, string(settings))

	// The account signs in; later runs keep its login and link new things.
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte("secret-2"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(claude, "skills"), 0o700))
	_, err = Prepare(config.TypeClaudeCode, "claude-2")
	require.NoError(t, err)
	require.True(t, SignedIn(config.TypeClaudeCode, "claude-2"))
	_, err = os.Stat(filepath.Join(dir, "skills"))
	require.NoError(t, err)

	argv, err := LoginArgv(config.TypeClaudeCode, "claude-2")
	require.NoError(t, err)
	require.Equal(t, []string{"env", "CLAUDE_CONFIG_DIR=" + dir, "claude", "auth", "login"}, argv)

	require.NoError(t, Remove("claude-2"))
	_, err = os.Lstat(dir)
	require.True(t, os.IsNotExist(err))
	for _, name := range []string{"settings.json", ".credentials.json", filepath.Join("projects", "-p", "s1.jsonl")} {
		_, err := os.Stat(filepath.Join(claude, name))
		require.NoError(t, err, "account 1's %s stays", name)
	}
	require.Error(t, Remove(""))
	require.Error(t, Remove("../x"))
}

func TestCodexAccountKeepsItsOwnLoginAndDaemon(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	t.Setenv("CRUSH_GLOBAL_DATA", filepath.Join(home, "data"))
	codex := filepath.Join(home, ".codex")
	for _, name := range []string{"sessions", "app-server-daemon"} {
		require.NoError(t, os.MkdirAll(filepath.Join(codex, name), 0o700))
	}
	for _, name := range []string{"auth.json", "config.toml", "state_5.sqlite"} {
		require.NoError(t, os.WriteFile(filepath.Join(codex, name), []byte("x"), 0o600))
	}
	dir, err := Prepare(config.TypeCodexCLI, "codex-2")
	require.NoError(t, err)
	for name, linked := range map[string]bool{"sessions": true, "config.toml": true, "state_5.sqlite": true, "auth.json": false, "app-server-daemon": false} {
		_, err := os.Lstat(filepath.Join(dir, name))
		require.Equal(t, linked, err == nil, name)
	}
	require.Equal(t, "codex-3", NewID(config.TypeCodexCLI, []config.Account{{ID: "codex-2", Kind: config.TypeCodexCLI}}))
}

// An account is named by the name it was given, else the email it signed in
// with (Claude's settings file, Codex's ID token), else its number; where
// two accounts end up with the same name, the label adds the tool, or the
// tool's number when both are the same tool's.
func TestNamesComeFromTheGivenNameThenTheEmail(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	t.Setenv("CRUSH_GLOBAL_DATA", filepath.Join(home, "data"))
	write := func(path, text string) {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte(text), 0o600))
	}
	token := func(claims string) string {
		return "eyJhbGciOiJSUzI1NiJ9." + base64.RawURLEncoding.EncodeToString([]byte(claims)) + ".c2ln"
	}
	write(filepath.Join(home, ".claude.json"), `{"oauthAccount":{"emailAddress":"you@example.com"},"mcpServers":{}}`)
	write(filepath.Join(home, ".codex", "auth.json"), `{"tokens":{"id_token":"`+token(`{"email":"you@example.com","name":"You"}`)+`"}}`)
	write(filepath.Join(home, "data", "accounts", "claude-2", ".claude.json"), `{"oauthAccount":{"emailAddress":"work@example.com"}}`)
	cfg := &config.Config{Options: &config.Options{Accounts: []config.Account{
		{ID: "claude-2", Kind: config.TypeClaudeCode},
		{ID: "codex-2", Kind: config.TypeCodexCLI},
	}}}

	require.Equal(t, "you@example.com", Email(config.TypeClaudeCode, ""))
	require.Equal(t, "you@example.com", Email(config.TypeCodexCLI, ""))
	require.Equal(t, "work@example.com", Email(config.TypeClaudeCode, "claude-2"))
	require.Empty(t, Email(config.TypeCodexCLI, "codex-2"), "not signed in yet")

	labels := func() map[string]string {
		out := map[string]string{}
		for _, a := range Every(cfg) {
			out[string(a.Kind)+"/"+a.ID] = a.Label
		}
		return out
	}
	require.Equal(t, map[string]string{
		"claude-code/":         "you@example.com (Claude)",
		"claude-code/claude-2": "work@example.com",
		"codex-cli/":           "you@example.com (Codex)",
		"codex-cli/codex-2":    "Codex 2",
	}, labels())
	require.Equal(t, "you@example.com", Name(cfg, config.TypeClaudeCode, ""))
	require.Equal(t, "you@example.com (Codex)", Label(cfg, config.TypeCodexCLI, ""))

	// A given name wins, for an added account and for a tool's own login.
	cfg.Options.OwnAccountNames = map[string]string{"claude-code": "Personal"}
	cfg.Options.Accounts[0].Name = "Personal"
	require.Equal(t, map[string]string{
		"claude-code/":         "Personal (Claude 1)",
		"claude-code/claude-2": "Personal (Claude 2)",
		"codex-cli/":           "you@example.com",
		"codex-cli/codex-2":    "Codex 2",
	}, labels())

	// A token that can't be read leaves the number.
	write(filepath.Join(home, ".codex", "auth.json"), `{"tokens":{"id_token":"not-a-token"}}`)
	require.Empty(t, Email(config.TypeCodexCLI, ""))
	require.Equal(t, "Codex 1", Name(cfg, config.TypeCodexCLI, ""))
}
