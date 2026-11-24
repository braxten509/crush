package cliagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"charm.land/catwalk/v3/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/accounts"
	"github.com/charmbracelet/crush/internal/config"
)

// SignedIn reports whether kind's own login is on this machine, read from
// the place the CLI keeps it: Grok's auth.json, OpenCode's auth.json, AGY's
// keyring entry. Only the CLIs that sign in themselves answer; every other
// kind is false, its credential being an account folder or an API key
// (owner, 2026-10-24: Settings → Accounts shows these four CLIs' state).
func SignedIn(kind catwalk.Type) bool {
	switch kind {
	case config.TypeGrokCLI:
		return fileHasBytes(filepath.Join(grokHome(), "auth.json"))
	case config.TypeOpenCodeCLI:
		return openCodeAuthHas("opencode-go")
	case config.TypeOpenCodeZenCLI:
		return openCodeAuthHas("opencode")
	case config.TypeAGYCLI:
		return accounts.SignedIn(kind, "")
	}
	return false
}

// grokHome is where Grok keeps its login: GROK_HOME, else ~/.grok.
func grokHome() string {
	if dir := os.Getenv("GROK_HOME"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".grok")
}

// openCodeDataDir is OpenCode's data folder: $XDG_DATA_HOME/opencode, else
// ~/.local/share/opencode.
func openCodeDataDir() string {
	dir := os.Getenv("XDG_DATA_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dir, "opencode")
}

// openCodeAuthHas reports whether OpenCode's auth.json holds a credential
// under key: "opencode-go" for OpenCode Go, "opencode" for OpenCode Zen.
func openCodeAuthHas(key string) bool {
	data, err := os.ReadFile(filepath.Join(openCodeDataDir(), "auth.json"))
	if err != nil {
		return false
	}
	var entries map[string]json.RawMessage
	if json.Unmarshal(data, &entries) != nil {
		return false
	}
	raw, ok := entries[key]
	return ok && len(bytes.TrimSpace(raw)) > 0 && string(bytes.TrimSpace(raw)) != "null"
}

// LoginEmail is the email kind's own login signed in with, where the CLI
// keeps one apart from its credential: Antigravity's active account in
// ~/.gemini/google_accounts.json. OpenCode keeps only a key and every other
// kind answers elsewhere, so they have none (owner, 2026-10-28).
func LoginEmail(kind catwalk.Type) string {
	if kind != config.TypeAGYCLI {
		return ""
	}
	return accounts.LoginEmail(kind, "")
}

func fileHasBytes(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}

// SignOut removes kind's own stored sign-in on this machine: the entry in
// OpenCode's auth.json for OpenCode Go or Zen, or the tool's own login for
// Claude Code, Codex, Grok and Antigravity.
func SignOut(ctx context.Context, kind catwalk.Type) error {
	switch kind {
	case config.TypeOpenCodeCLI:
		return removeOpenCodeAuth("opencode-go")
	case config.TypeOpenCodeZenCLI:
		return removeOpenCodeAuth("opencode")
	case config.TypeClaudeCode, config.TypeCodexCLI, config.TypeGrokCLI, config.TypeAGYCLI:
		return accounts.SignOutOwn(ctx, kind)
	default:
		return fmt.Errorf("cannot sign out of %s", kind)
	}
}

func removeOpenCodeAuth(key string) error {
	dir := openCodeDataDir()
	if dir == "" {
		return errors.New("could not find OpenCode data folder")
	}
	path := filepath.Join(dir, "auth.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("reading OpenCode auth.json: %w", err)
	}
	if _, ok := entries[key]; !ok {
		return nil
	}
	delete(entries, key)
	next, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	next = append(next, '\n')
	tmp, err := os.CreateTemp(dir, ".auth.json.crush-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(next); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}
