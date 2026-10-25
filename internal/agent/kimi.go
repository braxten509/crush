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
	// A folder inside a Git project that starts at the home folder.
	kimiHomeProjectMessage = "Moonshot AI is not usable here: this folder's git project is your home folder"
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
		return kimiHomeProjectMessage
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

// gitRoot is the nearest folder at or above dir that holds a real .git
// entry (a directory with a HEAD, or the file a worktree uses), or ""
// when there is none. An empty .git folder is not a project.
func gitRoot(dir string) string {
	if dir == "" || dir == string(filepath.Separator) {
		return ""
	}
	if isGitDir(filepath.Join(dir, ".git")) {
		return dir
	}
	parent := filepath.Dir(dir)
	if parent == dir {
		return ""
	}
	return gitRoot(parent)
}

func isGitDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if !info.IsDir() {
		return true
	}
	_, err = os.Stat(filepath.Join(path, "HEAD"))
	return err == nil
}
