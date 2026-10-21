// Package accounts keeps the folders of extra Claude Code and Codex
// sign-ins (config.Account). Each account folder holds its own login; every
// other file and folder links to the tool's own folder (account 1), so an
// account has the same settings, skills, plugins, MCP tools and saved
// sessions, and a chat moved between accounts resumes with its full memory.
package accounts

import (
	"bytes"
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

// syncClaudeSettings copies the MCP servers of Claude's own settings file
// (~/.claude.json, which also holds the login's account) into the
// account's. Claude keeps that file in the account folder when it runs
// with CLAUDE_CONFIG_DIR.
func syncClaudeSettings(dir string) error {
	h, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	srcPath := filepath.Join(h, ".claude.json")
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		srcPath = filepath.Join(d, ".claude.json")
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
	}
	return nil, fmt.Errorf("%s has no accounts", kind)
}

// SignedIn reports whether an account folder holds a login.
func SignedIn(kind catwalk.Type, id string) bool {
	dir := config.AccountDir(id)
	if dir == "" {
		return true
	}
	name := ".credentials.json"
	if kind == config.TypeCodexCLI {
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
