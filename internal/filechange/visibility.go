package filechange

import (
	"os"
	"path/filepath"
	"strings"
)

// HiddenReviewPath applies the same visibility rules to saved summaries and
// full reviews. It never reads or enumerates the filesystem.
func HiddenReviewPath(path, root string) bool {
	if root != "" && !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path = filepath.Clean(path)
	home, _ := os.UserHomeDir()
	if hiddenFolderPath(path, home) || scratchFolderPath(path) || agentToolPath(path) {
		return true
	}
	cache, _ := os.UserCacheDir()
	configuration, _ := os.UserConfigDir()
	// Devices and kernel files (/dev/tty, /sys/fs/cgroup/...) are written by
	// the programs a command starts; they are never edits.
	excluded := []string{os.TempDir(), cache, configuration, "/tmp", "/var/tmp", "/dev", "/proc", "/sys"}
	// Application data and state are noise too, including shared agent memory
	// and sessions.
	for _, location := range []struct{ variable, fallback string }{
		{"XDG_DATA_HOME", "share"},
		{"XDG_STATE_HOME", "state"},
	} {
		directory := os.Getenv(location.variable)
		if !filepath.IsAbs(directory) && home != "" {
			directory = filepath.Join(home, ".local", location.fallback)
		}
		if filepath.IsAbs(directory) {
			excluded = append(excluded, directory)
		}
	}
	for _, directory := range excluded {
		if directory == "" {
			continue
		}
		directory = filepath.Clean(directory)
		if path == directory || strings.HasPrefix(path, directory+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// Instruction files the user reads and edits stay visible even inside an
// agent's folder.
var instructionFiles = map[string]bool{
	"AGENTS.md": true, "CLAUDE.md": true, "CLAUDE.local.md": true, "CRUSH.md": true, "GEMINI.md": true,
}

// hiddenFolderPath reports a path inside any folder whose name starts with a
// dot (.git, .claude-flow, .github, ...). Dot files themselves (.gitignore,
// .env) stay visible, and so does ~/.local/bin, which holds user-written tools.
func hiddenFolderPath(path, home string) bool {
	if instructionFiles[filepath.Base(path)] {
		return false
	}
	if home != "" {
		tools := filepath.Join(home, ".local", "bin")
		if path == tools || strings.HasPrefix(path, tools+string(filepath.Separator)) {
			return false
		}
	}
	folders := strings.Split(filepath.Dir(path), string(filepath.Separator))
	for _, folder := range folders {
		if strings.HasPrefix(folder, ".") && folder != "." && folder != ".." {
			return true
		}
	}
	return false
}

// Throwaway work folders: experiments, copies and temporary output.
var scratchFolders = map[string]bool{"scratch": true, "tmp": true, "temp": true}

// scratchFolderPath reports a path inside a folder named scratch, tmp or temp
// at any depth (~/scratch/app/..., project/tmp/out.txt).
func scratchFolderPath(path string) bool {
	for _, folder := range strings.Split(filepath.Dir(path), string(filepath.Separator)) {
		if scratchFolders[strings.ToLower(folder)] {
			return true
		}
	}
	return false
}

// agentToolPath reports state that agent tooling keeps outside dot folders:
// agent databases, skill lockfiles and swarm runtimes.
func agentToolPath(path string) bool {
	if instructionFiles[filepath.Base(path)] {
		return false
	}
	for _, component := range strings.Split(path, string(filepath.Separator)) {
		name := strings.ToLower(component)
		if name == "skills-lock.json" || strings.HasPrefix(name, "agentdb.") ||
			strings.HasPrefix(name, "claude-flow") || strings.HasPrefix(name, "ruflo") {
			return true
		}
	}
	return false
}
