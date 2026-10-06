package cliagent

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"charm.land/catwalk/pkg/catwalk"

	"github.com/charmbracelet/crush/internal/config"
)

// Read-only sub-agents run their CLI inside Codex's sandbox with nothing
// writable but the temporary folder, the cache and the CLI's own state, so
// neither its edit tools nor its commands can change the project. Codex
// sub-agents use the same sandbox through their own read-only mode.

// readOnlyStateDirs are the folders, under home, where each CLI keeps the
// login, history and settings it must still write.
var readOnlyStateDirs = map[catwalk.Type][]string{
	config.TypeClaudeCode:  {".claude", ".local/share/claude", ".local/state/claude"},
	config.TypeGrokCLI:     {".grok"},
	config.TypeOpenCodeCLI: {".local/share/opencode", ".local/state/opencode", ".config/opencode"},
	config.TypeAGYCLI:      {".gemini"},
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

// readOnlyCommand wraps a CLI command so it runs read-only. The sandbox
// makes its own working folder writable, so it starts in the temporary
// folder (writable anyway) and the CLI moves into the project itself.
func (m *Model) readOnlyCommand(name string, args []string) (string, string, []string, error) {
	if _, err := exec.LookPath("codex"); err != nil {
		return "", "", nil, errors.New("read-only sub-agents run inside Codex's sandbox, and codex isn't installed")
	}
	tmp := os.TempDir()
	if within(m.Dir, tmp) {
		return "", "", nil, fmt.Errorf("can't keep %s read-only: the sandbox always lets programs write in %s", m.Dir, tmp)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", nil, err
	}
	var roots []string
	dirs := append([]string{".cache"}, readOnlyStateDirs[m.Kind]...)
	for _, dir := range dirs {
		dir = filepath.Join(home, dir)
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() && !within(m.Dir, dir) {
			roots = append(roots, fmt.Sprintf("%q", dir))
		}
	}
	wrapped := []string{
		"sandbox",
		"-c", `sandbox_mode="workspace-write"`,
		"-c", "sandbox_workspace_write.network_access=true",
		"-c", "sandbox_workspace_write.writable_roots=[" + strings.Join(roots, ",") + "]",
		"--", "/bin/sh", "-c", `cd "$0" && exec "$@"`, m.Dir, name,
	}
	return tmp, "codex", append(wrapped, args...), nil
}

// within reports whether path is dir or inside it.
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
