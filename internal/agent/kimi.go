package agent

import (
	"os"
	"path/filepath"

	"charm.land/catwalk/pkg/catwalk"
)

// Kimi's plans are billed per project, so the provider only runs inside
// a Git project, and never in the home folder itself (owner,
// 2026-10-24). The same reason greys its rows out in the model menu.
const KimiProviderID = string(catwalk.InferenceKimiCoding)

const (
	kimiNoGitMessage = "Moonshot AI is not usable here: this folder is not a git project"
	kimiHomeMessage  = "Moonshot AI is not usable here: this is your home folder"
)

// KimiUnusable returns why a provider cannot run in dir, or "" when it
// can. Only Kimi has a restriction today.
func KimiUnusable(provider, dir string) string {
	home, _ := os.UserHomeDir()
	return kimiUnusableAt(provider, dir, home)
}

// kimiUnusableAt is KimiUnusable with the home folder passed in, so tests
// never depend on the machine's own home.
func kimiUnusableAt(provider, dir, home string) string {
	if provider != KimiProviderID {
		return ""
	}
	dir = canonicalPath(dir)
	home = canonicalPath(home)
	if home != "" && dir == home {
		return kimiHomeMessage
	}
	root := gitRoot(dir)
	if root == "" {
		return kimiNoGitMessage
	}
	if home != "" && root == home {
		return kimiHomeMessage
	}
	return ""
}

// canonicalPath makes a path comparable, following symlinks when it can.
func canonicalPath(path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = filepath.Clean(path)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	return filepath.Clean(abs)
}

// gitRoot is the nearest folder at or above dir that holds a .git entry
// (a directory, or the file a worktree uses), or "" when there is none.
func gitRoot(dir string) string {
	if dir == "" || dir == string(filepath.Separator) {
		return ""
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return dir
	}
	parent := filepath.Dir(dir)
	if parent == dir {
		return ""
	}
	return gitRoot(parent)
}
