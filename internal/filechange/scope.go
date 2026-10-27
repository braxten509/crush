package filechange

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// RepositoryRoot finds the current worktree without inventorying its files.
// Both ordinary .git directories and linked-worktree .git files are supported.
func RepositoryRoot(directory string) string {
	if directory == "" {
		return ""
	}
	directory, err := filepath.Abs(directory)
	if err != nil {
		return ""
	}
	directory, err = filepath.EvalSymlinks(directory)
	if err != nil {
		return ""
	}
	for {
		if isGitDir(filepath.Join(directory, ".git")) {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return ""
		}
		directory = parent
	}
}

// isGitDir reports whether path is a linked worktree's .git file or a .git
// folder git would use: one holding HEAD. An empty .git folder is no
// repository (git says so too).
func isGitDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.Mode().IsRegular() {
		return true
	}
	if !info.IsDir() {
		return false
	}
	_, err = os.Stat(filepath.Join(path, "HEAD"))
	return err == nil
}

// InRepository checks the actual parent directory, so directory symlinks do
// not pull another project into this project's review. File symlinks must also stay
// inside it: native tools read and write through their final component.
func InRepository(repository, workingDirectory, path string) bool {
	if repository == "" || path == "" {
		return false
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(workingDirectory, path)
	}
	if !within(canonicalParent(path), repository) {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return within(resolved, repository)
	}
	return true
}

// ScopeReview applies the same boundary to tool-supplied and observed changes.
// Whole-file deletions carry a flag, never a file list or recovery contents.
func ScopeReview(review *Review, workingDirectory string) *Review {
	if review == nil {
		return nil
	}
	scoped := *review
	scoped.Changes = nil
	repository := RepositoryRoot(workingDirectory)
	sourceRoot := review.Root
	if sourceRoot == "" {
		sourceRoot = workingDirectory
	}
	for _, change := range review.Changes {
		if change.Before != nil && change.After == nil {
			scoped.Deletions = true
			continue
		}
		if InRepository(repository, sourceRoot, change.Path) {
			scoped.Changes = append(scoped.Changes, change)
		}
	}
	return &scoped
}

// WithoutDetails drops duplicate diff payloads while keeping execution output.
func WithoutDetails(metadata string) string {
	var values map[string]json.RawMessage
	if json.Unmarshal([]byte(metadata), &values) != nil {
		return metadata
	}
	for _, key := range []string{"old_content", "new_content", "diff", "additions", "removals"} {
		delete(values, key)
	}
	data, _ := json.Marshal(values)
	return string(data)
}
