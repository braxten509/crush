// Package prompthistory keeps the prompts the user sent, from every folder
// and every Crush app (terminal and desktop), in one shared file. ↑ in the
// editor walks it, newest first.
package prompthistory

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/jsonfile"
)

// Limit is how many prompts the file keeps.
const Limit = 100

// Path is the shared file, beside the global data file.
func Path() string {
	return filepath.Join(filepath.Dir(config.GlobalConfigData()), "prompt-history.json")
}

// shared reports whether the real file may be used: tests that don't
// point CRUSH_GLOBAL_DATA elsewhere never touch the user's history.
func shared() bool {
	return !testing.Testing() || os.Getenv("CRUSH_GLOBAL_DATA") != ""
}

// Add saves a sent prompt (shell commands as "!command"). Blank ones are
// skipped.
func Add(text string) {
	if !shared() {
		return
	}
	if err := add(Path(), text); err != nil {
		slog.Warn("Failed to save prompt history", "error", err)
	}
}

// List returns the saved prompts, newest first.
func List() []string {
	if !shared() {
		return nil
	}
	return list(Path())
}

// The file holds the prompts oldest first.
func add(path, text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return jsonfile.Update(path, func(saved *[]string) bool {
		*saved = append(*saved, text)
		if extra := len(*saved) - Limit; extra > 0 {
			*saved = (*saved)[extra:]
		}
		return true
	})
}

func list(path string) []string {
	saved := jsonfile.Read[[]string](path)
	out := make([]string, 0, len(saved))
	for i := len(saved) - 1; i >= 0; i-- {
		out = append(out, saved[i])
	}
	return out
}
