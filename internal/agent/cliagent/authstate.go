package cliagent

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
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
		return agySignedIn()
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

// agySignedIn reports whether AGY's login is in the Secret Service keyring,
// where it keeps it (agyToken). A test never asks the real keyring, which
// may prompt to be unlocked.
func agySignedIn() bool {
	if testing.Testing() {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "secret-tool", "lookup", "service", "gemini", "username", "antigravity").Output()
	return err == nil && len(bytes.TrimSpace(out)) > 0
}

func fileHasBytes(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}
