// Package accounts keeps the folders of extra Claude Code and Codex
// sign-ins of the tools that take more than one (config.Account). Each
// account folder holds its own login; every other file and folder links to
// the tool's own folder (account 1), so an account has the same settings,
// skills, plugins, MCP tools and saved sessions, and a chat moved between
// accounts resumes with its full memory.
package accounts

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// own lists what each tool keeps per account: never linked.
var own = map[catwalk.Type][]string{
	config.TypeClaudeCode: {".credentials.json", ".claude.json", "daemon", "bridge-spawn", "statsig"},
	config.TypeCodexCLI:   {"auth.json", "app-server-control", "app-server-daemon", "ipc", "process_manager", ".sqlite-maintenance.lock"},
	config.TypeGrokCLI:    {"auth.json", "auth.json.lock", "active_sessions.json", "active_sessions.lock"},
}

// ownPrefixes are per-account lock files.
var ownPrefixes = []string{".oauth_refresh", ".lock"}

// home is the tool's own folder, account 1's.
func home(kind catwalk.Type) (string, error) {
	switch kind {
	case config.TypeClaudeCode:
		if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
			return dir, nil
		}
		h, err := os.UserHomeDir()
		return filepath.Join(h, ".claude"), err
	case config.TypeCodexCLI:
		if dir := os.Getenv("CODEX_HOME"); dir != "" {
			return dir, nil
		}
		h, err := os.UserHomeDir()
		return filepath.Join(h, ".codex"), err
	case config.TypeGrokCLI:
		if dir := os.Getenv("GROK_HOME"); dir != "" {
			return dir, nil
		}
		h, err := os.UserHomeDir()
		return filepath.Join(h, ".grok"), err
	}
	return "", fmt.Errorf("%s has no accounts", kind)
}

// Prepare makes an account's folder, or brings it up to date: whatever the
// tool's own folder gained since is linked in, and Claude's MCP servers are
// copied into the account's own settings file. It returns the folder.
func Prepare(kind catwalk.Type, id string) (string, error) {
	dir := config.AccountDir(id)
	if dir == "" {
		return "", errors.New("account 1 is the tool's own folder")
	}
	src, err := home(kind)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(src)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	for _, e := range entries {
		name := e.Name()
		if slices.Contains(own[kind], name) || slices.ContainsFunc(ownPrefixes, func(p string) bool { return strings.HasPrefix(name, p) }) {
			continue
		}
		target := filepath.Join(dir, name)
		if _, err := os.Lstat(target); err == nil {
			continue // linked before, or the account's own
		}
		if err := os.Symlink(filepath.Join(src, name), target); err != nil {
			return "", err
		}
	}
	if kind == config.TypeClaudeCode {
		if err := syncClaudeSettings(dir); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// claudeSettingsFile is Claude Code's own settings file for an account,
// which also holds the login's account: ~/.claude.json for account 1 (in
// CLAUDE_CONFIG_DIR when that is set), the account folder's otherwise.
// Claude keeps that file in the account folder when it runs with
// CLAUDE_CONFIG_DIR.
func claudeSettingsFile(id string) (string, error) {
	if dir := config.AccountDir(id); dir != "" {
		return filepath.Join(dir, ".claude.json"), nil
	}
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, ".claude.json"), nil
	}
	h, err := os.UserHomeDir()
	return filepath.Join(h, ".claude.json"), err
}

// syncClaudeSettings copies the MCP servers of account 1's Claude settings
// file into the account's.
func syncClaudeSettings(dir string) error {
	srcPath, err := claudeSettingsFile("")
	if err != nil {
		return err
	}
	src, err := os.ReadFile(srcPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	servers := gjson.GetBytes(src, "mcpServers")
	if !servers.Exists() {
		return nil
	}
	path := filepath.Join(dir, ".claude.json")
	cur, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(bytes.TrimSpace(cur)) == 0 {
		cur = []byte("{}")
	}
	if !json.Valid(cur) {
		return nil // Claude is mid-write; the next turn syncs it
	}
	if gjson.GetBytes(cur, "mcpServers").Raw == servers.Raw {
		return nil
	}
	next, err := sjson.SetRawBytes(cur, "mcpServers", []byte(servers.Raw))
	if err != nil {
		return err
	}
	tmp := path + ".crush-tmp"
	if err := os.WriteFile(tmp, next, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// LoginArgv is the command that signs an account in, in a terminal.
func LoginArgv(kind catwalk.Type, id string) ([]string, error) {
	env := config.AccountEnv(kind, id)
	if len(env) == 0 {
		return nil, errors.New("account 1 signs in with the tool itself")
	}
	argv := append([]string{"env"}, env...)
	switch kind {
	case config.TypeClaudeCode:
		return append(argv, "claude", "auth", "login"), nil
	case config.TypeCodexCLI:
		return append(argv, "codex", "login"), nil
	case config.TypeGrokCLI:
		return append(argv, "grok", "login"), nil
	}
	return nil, fmt.Errorf("%s has no accounts", kind)
}

// Email is the email an account signed in with, read from the tool's own
// files, or "" while it has none or they can't be read: Claude Code keeps it
// in its settings file (oauthAccount.emailAddress), Codex in the ID token
// of auth.json (the token's email claim).
func Email(kind catwalk.Type, id string) string {
	switch kind {
	case config.TypeClaudeCode:
		path, err := claudeSettingsFile(id)
		if err != nil {
			return ""
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(gjson.GetBytes(data, "oauthAccount.emailAddress").String())
	case config.TypeCodexCLI:
		dir := config.AccountDir(id)
		if dir == "" {
			var err error
			if dir, err = home(kind); err != nil {
				return ""
			}
		}
		data, err := os.ReadFile(filepath.Join(dir, "auth.json"))
		if err != nil {
			return ""
		}
		parts := strings.Split(gjson.GetBytes(data, "tokens.id_token").String(), ".")
		if len(parts) != 3 {
			return ""
		}
		claims, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
		if err != nil {
			return ""
		}
		return strings.TrimSpace(gjson.GetBytes(claims, "email").String())
	}
	return ""
}

// Name is an account's shown name: the name it was given, else the email
// it signed in with, else its tool and number ("Claude 2").
func Name(cfg *config.Config, kind catwalk.Type, id string) string {
	if name := cfg.AccountGivenName(kind, id); name != "" {
		return name
	}
	if email := Email(kind, id); email != "" {
		return email
	}
	return cfg.AccountNumberName(kind, id)
}

// Shown is one account as it is named.
type Shown struct {
	Kind catwalk.Type
	ID   string // "" for the tool's own login
	Name string
	// Label is Name where accounts of both tools are named together (the
	// fallback order, a chat's notes when it switches accounts): another
	// account with the same name adds the tool, "you@gmail.com (Codex)", or
	// the tool and number when that account is the same tool's (owner,
	// 2026-10-21).
	Label string
}

// Every lists each tool's own login, then its added accounts, named.
func Every(cfg *config.Config) []Shown {
	var all []Shown
	for _, kind := range config.AccountKinds {
		all = append(all, Shown{Kind: kind, Name: Name(cfg, kind, "")})
		for _, a := range cfg.Accounts(kind) {
			all = append(all, Shown{Kind: kind, ID: a.ID, Name: Name(cfg, kind, a.ID)})
		}
	}
	for i, me := range all {
		clash, sameTool := false, false
		for j, other := range all {
			if j != i && strings.EqualFold(other.Name, me.Name) {
				clash = true
				sameTool = sameTool || other.Kind == me.Kind
			}
		}
		switch {
		case sameTool:
			all[i].Label = fmt.Sprintf("%s (%s)", me.Name, cfg.AccountNumberName(me.Kind, me.ID))
		case clash:
			all[i].Label = fmt.Sprintf("%s (%s)", me.Name, config.AccountKindName(me.Kind))
		default:
			all[i].Label = me.Name
		}
	}
	return all
}

// Label is an account's label (Shown.Label).
func Label(cfg *config.Config, kind catwalk.Type, id string) string {
	for _, s := range Every(cfg) {
		if s.Kind == kind && s.ID == id {
			return s.Label
		}
	}
	return Name(cfg, kind, id)
}

// SignedIn reports whether an account folder holds a login.
func SignedIn(kind catwalk.Type, id string) bool {
	dir := config.AccountDir(id)
	if dir == "" {
		return true
	}
	name := ".credentials.json"
	if kind == config.TypeCodexCLI || kind == config.TypeGrokCLI {
		name = "auth.json"
	}
	info, err := os.Stat(filepath.Join(dir, name))
	return err == nil && info.Size() > 0
}

// NewID is a fresh account ID for kind, like "claude-2".
func NewID(kind catwalk.Type, taken []config.Account) string {
	base := strings.ToLower(config.AccountKindName(kind))
	for n := 2; ; n++ {
		id := fmt.Sprintf("%s-%d", base, n)
		if !slices.ContainsFunc(taken, func(a config.Account) bool { return a.ID == id }) {
			if _, err := os.Lstat(config.AccountDir(id)); os.IsNotExist(err) {
				return id
			}
		}
	}
}

// Remove deletes an account's folder: its login and the links in it. The
// linked files themselves belong to account 1 and stay.
func Remove(id string) error {
	dir := config.AccountDir(id)
	if dir == "" || filepath.Dir(dir) != config.AccountsDir() {
		return errors.New("not an account folder")
	}
	return os.RemoveAll(dir) // RemoveAll removes links, never what they point at
}
