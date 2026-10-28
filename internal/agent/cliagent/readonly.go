package cliagent

import (
	"os"
	"path/filepath"

	"charm.land/catwalk/pkg/catwalk"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/sandbox"
)

// Read-only sub-agents run their CLI inside Codex's sandbox with nothing
// writable but the temporary folder, the cache and the CLI's own state, so
// neither its edit tools nor its commands can change the project. Codex
// sub-agents use the same sandbox through their own read-only mode.

// readOnlyStateDirs are the folders, under home, where each CLI keeps the
// login, history and settings it must still write.
var readOnlyStateDirs = map[catwalk.Type][]string{
	config.TypeClaudeCode:     {".claude", ".local/share/claude", ".local/state/claude"},
	config.TypeGrokCLI:        {".grok"},
	config.TypeOpenCodeCLI:    {".local/share/opencode", ".local/state/opencode", ".config/opencode"},
	config.TypeOpenCodeZenCLI: {".local/share/opencode", ".local/state/opencode", ".config/opencode"},
	config.TypeAGYCLI:         {".gemini"},
}

// startTurnProc starts the CLI for a turn: read-only when the model is,
// and otherwise with its file changes recorded for review.
func (m *Model) startTurnProc(noTools bool, env []string, name string, args ...string) (*proc, error) {
	if !m.ReadOnly || noTools {
		return startReviewProc(m.Dir, env, !noTools, name, args...)
	}
	dir, name, args, err := m.readOnlyCommand(name, args)
	if err != nil {
		return nil, err
	}
	// Nothing in the project can change, so there is nothing to review.
	return startReviewProc(dir, env, false, name, args...)
}

// readOnlyCommand wraps a CLI command so it runs read-only, with its own
// state still writable.
func (m *Model) readOnlyCommand(name string, args []string) (string, string, []string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", nil, err
	}
	var writable []string
	for _, dir := range readOnlyStateDirs[m.Kind] {
		writable = append(writable, filepath.Join(home, dir))
	}
	// An added account's folder is the CLI's state while it runs there.
	if dir := config.AccountDir(m.Account); dir != "" {
		writable = append(writable, dir)
	}
	return sandbox.ReadOnly(m.Dir, writable, name, args)
}
