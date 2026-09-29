//go:build linux

package cliagent

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/charlievieth/fastwalk"
)

// instructionCommand gives CLIs a private view of instruction files. It does
// not hide configuration, authentication, skills, hooks or command rules.
// Bubblewrap is required: failing to isolate must not silently load rules.
func instructionCommand(dir, home, name string, args ...string) (*exec.Cmd, func(), error) {
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		return nil, nil, fmt.Errorf("Crush's shared-only instructions require bubblewrap: %w", err)
	}
	bin, err := exec.LookPath(name)
	if err != nil {
		return nil, nil, err
	}
	masks, err := instructionMasks(dir, home)
	if err != nil {
		return nil, nil, err
	}
	view, err := getInstructionView()
	if err != nil {
		return nil, nil, err
	}
	// An argument file avoids ARG_MAX in large workspaces. No instructions or
	// secrets are copied into it; it contains mount paths only.
	f, err := os.CreateTemp("", "crush-instruction-view-*")
	if err != nil {
		return nil, nil, err
	}
	files := []*os.File{f}
	cleanup := func() {
		for _, file := range files {
			_ = file.Close()
			_ = os.Remove(file.Name())
		}
	}
	options := []string{"--bind", "/", "/", "--dev-bind", "/dev", "/dev"}
	for _, path := range masks {
		source, err := view.source(path, false)
		if err != nil {
			cleanup()
			return nil, nil, err
		}
		options = append(options, "--bind", source, path)
	}
	// OpenCode also accepts arbitrary filenames and URLs in config. Replace
	// only this prompt field in the private view, preserving permission rules,
	// authentication, hooks, providers and all other settings.
	configRoot := os.Getenv("XDG_CONFIG_HOME")
	if configRoot == "" {
		configRoot = filepath.Join(home, ".config")
	}
	configs := []string{filepath.Join(configRoot, "opencode", "opencode.json"), filepath.Join(configRoot, "opencode", "opencode.jsonc")}
	if extra := os.Getenv("OPENCODE_CONFIG"); extra != "" {
		configs = append(configs, extra)
	}
	for root := dir; root != ""; root = filepath.Dir(root) {
		for _, sub := range []string{"opencode.json", "opencode.jsonc", ".opencode/opencode.json", ".opencode/opencode.jsonc"} {
			configs = append(configs, filepath.Join(root, sub))
		}
		if filepath.Dir(root) == root {
			break
		}
	}
	for _, path := range configs {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}
		source, err := view.source(path, true)
		if err != nil {
			cleanup()
			return nil, nil, err
		}
		options = append(options, "--bind", source, path)
	}
	if _, err = f.WriteString(strings.Join(options, "\x00") + "\x00"); err != nil {
		cleanup()
		return nil, nil, err
	}
	if _, err = f.Seek(0, 0); err != nil {
		cleanup()
		return nil, nil, err
	}
	cmd := exec.Command(bwrap, append([]string{"--args", "3", "--", bin}, args...)...)
	cmd.ExtraFiles = files
	return cmd, cleanup, nil
}

func instructionMasks(dir, home string) ([]string, error) {
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	shared := []string{filepath.Join(home, ".agents", "skills"), filepath.Join(home, ".local", "share", "agent-memory")}
	if data := os.Getenv("XDG_DATA_HOME"); data != "" {
		shared = append(shared, filepath.Join(data, "agent-memory"))
	}
	protected := func(path string) bool {
		for _, root := range shared {
			if path == root || strings.HasPrefix(path, root+string(os.PathSeparator)) {
				return true
			}
		}
		return false
	}
	set := map[string]bool{}
	var masksMu sync.Mutex
	add := func(path string) error {
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			return nil
		} else if err != nil {
			return err
		}
		real, err := filepath.EvalSymlinks(path)
		if os.IsNotExist(err) {
			return nil // A dangling link cannot supply instructions.
		}
		if err != nil {
			return err
		}
		if protected(real) {
			return fmt.Errorf("instruction path %s points into shared memory or skills; refusing to hide the shared source", path)
		}
		masksMu.Lock()
		set[real] = true
		masksMu.Unlock()
		return nil
	}
	// Only Markdown guidance directories are masked. In particular, Codex's
	// rules/ directory contains executable command policy and stays visible.
	guidance := []string{".claude/rules", ".grok/rules", ".cursor/rules", ".agent/rules", ".agents/rules", ".gemini/rules", ".gemini/config/rules"}
	at := func(root string) error {
		for _, file := range instructionNames {
			if err := add(filepath.Join(root, file)); err != nil {
				return err
			}
		}
		for _, sub := range guidance {
			if err := add(filepath.Join(root, sub)); err != nil {
				return err
			}
		}
		return nil
	}
	// Ancestors and personal roots are loaded even when outside the workspace.
	for root := dir; ; root = filepath.Dir(root) {
		if err := at(root); err != nil {
			return nil, err
		}
		if filepath.Dir(root) == root {
			break
		}
	}
	roots := []string{home, filepath.Join(home, ".config", "opencode")}
	for _, key := range []string{"CODEX_HOME", "CLAUDE_CONFIG_DIR", "GROK_HOME", "OPENCODE_CONFIG_DIR"} {
		if root := os.Getenv(key); root != "" {
			roots = append(roots, root)
		}
	}
	for _, sub := range []string{".codex", ".claude", ".grok", ".cursor", ".gemini", ".gemini/config", ".abacusai", ".opencode", ".agents"} {
		roots = append(roots, filepath.Join(home, sub))
	}
	for _, root := range roots {
		if err := at(root); err != nil {
			return nil, err
		}
	}
	// Subdirectory instructions can be loaded later by a read tool. Exclude
	// private CLI state and caches, which are not project instruction roots.
	err = fastwalk.Walk(nil, dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsPermission(walkErr) || os.IsNotExist(walkErr) {
				return nil // The CLI cannot discover these paths either.
			}
			return walkErr
		}
		for _, sub := range guidance {
			if strings.HasSuffix(filepath.ToSlash(path), "/"+sub) {
				if err := add(path); err != nil {
					return err
				}
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if entry.IsDir() {
			if protected(path) || (path != dir && slices.Contains([]string{".git", ".cache", ".local", "node_modules", ".venv", ".gradle", ".next"}, entry.Name())) {
				return filepath.SkipDir
			}
			// Personal roots were checked explicitly above. Scanning the home
			// workspace must not crawl package caches, toolchains and sessions.
			if path != dir && filepath.Dir(path) == home && strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			if path != dir && slices.Contains([]string{".codex", ".grok", ".gemini", ".abacusai"}, entry.Name()) {
				if err := at(path); err != nil {
					return err
				}
				if entry.Name() != ".codex" {
					if err := add(filepath.Join(path, "rules")); err != nil {
						return err
					}
				}
				return filepath.SkipDir
			}
			masksMu.Lock()
			masked := set[path]
			masksMu.Unlock()
			if masked {
				return filepath.SkipDir
			}
			return nil
		}
		if slices.Contains(instructionNames, entry.Name()) || (entry.Name() == "copilot-instructions.md" && filepath.Base(filepath.Dir(path)) == ".github") {
			return add(path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("discover instruction files: %w", err)
	}
	paths := make([]string, 0, len(set))
	for path := range set {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	return paths, nil
}

var instructionNames = []string{
	"AGENTS.md", "AGENTS.override.md", "AGENT.md", "Agents.md", "Claude.md",
	"CLAUDE.md", "CLAUDE.local.md", "GEMINI.md", "GEMINI.local.md",
	"CRUSH.md", "CRUSH.local.md", ".cursorrules", ".windsurfrules", ".clinerules",
}
