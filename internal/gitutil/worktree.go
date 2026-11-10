package gitutil

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// ErrDirtyWorktree means a chat's folder copy has unsaved file changes and
// must not be removed automatically.
var ErrDirtyWorktree = errors.New("chat folder copy has unsaved file changes")

// ErrNotGitRepo means the folder is not inside a Git repository.
var ErrNotGitRepo = errors.New("folder is not a Git repository")

// Branches lists the local Git branches in dir, sorted by name, with the
// current branch included even before the first commit. Returns nil when dir
// is not a Git repository.
func Branches(dir string) []string {
	if findGitDir(dir) == "" {
		return nil
	}
	out, err := execGit(dir, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if err != nil {
		return nil
	}
	var names []string
	for line := range strings.SplitSeq(out, "\n") {
		name := strings.TrimSpace(line)
		if name != "" && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	if cur := CurrentBranch(dir); cur != "" && !slices.Contains(names, cur) {
		names = append(names, cur)
	}
	slices.Sort(names)
	return names
}

// ChatWorktreeDir returns the path where sessionID's separate folder copy
// lives under dataDir.
func ChatWorktreeDir(dataDir, sessionID string) string {
	return filepath.Join(dataDir, "worktrees", sessionID)
}

// SwitchChatBranch switches or creates branch for sessionID. When branch is
// the main folder's current branch (and create is false), the chat returns to
// the main folder and its extra folder copy is removed if clean. Otherwise
// the chat gets or updates its own folder copy under dataDir/worktrees.
func SwitchChatBranch(mainDir, dataDir, sessionID, branch string, create bool) (string, string, error) {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return "", "", errors.New("branch name is required")
	}
	if sessionID == "" {
		return "", "", errors.New("chat ID is required")
	}
	mainAbs, err := filepath.Abs(mainDir)
	if err != nil {
		return "", "", err
	}
	if findGitDir(mainAbs) == "" {
		return "", "", ErrNotGitRepo
	}
	dataAbs, err := filepath.Abs(dataDir)
	if err != nil {
		return "", "", err
	}
	wtDir := ChatWorktreeDir(dataAbs, sessionID)
	mainBranch := CurrentBranch(mainAbs)

	if !create && branch == mainBranch {
		if err := CleanupChatWorktree(mainAbs, wtDir); err != nil {
			return "", "", err
		}
		return branch, "", nil
	}

	if hasWorktree(wtDir) {
		args := []string{"checkout", branch}
		if create {
			args = []string{"checkout", "-b", branch}
		}
		if _, err := execGit(wtDir, args...); err != nil {
			return "", "", err
		}
		actual := CurrentBranch(wtDir)
		if actual == "" {
			actual = branch
		}
		return actual, wtDir, nil
	}

	if err := os.MkdirAll(filepath.Dir(wtDir), 0o755); err != nil {
		return "", "", fmt.Errorf("preparing worktrees folder: %w", err)
	}
	cleanStaleWorktreeAdmin(mainAbs, sessionID, wtDir)

	var args []string
	if create {
		args = []string{"worktree", "add", "-b", branch, wtDir}
	} else {
		args = []string{"worktree", "add", wtDir, branch}
	}
	if _, err := execGit(mainAbs, args...); err != nil {
		return "", "", err
	}
	actual := CurrentBranch(wtDir)
	if actual == "" {
		actual = branch
	}
	return actual, wtDir, nil
}

// EnsureChatWorktree returns the directory a chat should run in. If the chat
// has a branch of its own whose folder copy was removed when the chat closed,
// EnsureChatWorktree recreates it.
func EnsureChatWorktree(mainDir, dataDir, sessionID, branch, workDir string) (string, string) {
	if branch == "" {
		return mainDir, ""
	}
	if workDir != "" && hasWorktree(workDir) {
		if cur := CurrentBranch(workDir); cur != "" {
			return workDir, cur
		}
		return workDir, branch
	}
	if CurrentBranch(mainDir) == branch {
		return mainDir, branch
	}
	actual, wtDir, err := SwitchChatBranch(mainDir, dataDir, sessionID, branch, false)
	if err != nil || wtDir == "" {
		return mainDir, CurrentBranch(mainDir)
	}
	return wtDir, actual
}

// IsolateMainBranchSwitch checks whether a turn running in mainDir switched
// the checked-out branch away from prevBranch. When it did, mainDir is put
// back on prevBranch and the new branch is moved into sessionID's own folder
// copy so other chats in mainDir stay on prevBranch.
func IsolateMainBranchSwitch(mainDir, dataDir, sessionID, prevBranch string) (newBranch, workDir string, moved bool) {
	if prevBranch == "" || sessionID == "" || dataDir == "" {
		return "", "", false
	}
	curBranch := CurrentBranch(mainDir)
	if curBranch == "" || curBranch == prevBranch {
		return "", "", false
	}
	if _, err := execGit(mainDir, "checkout", "-q", prevBranch); err != nil {
		return curBranch, "", false
	}
	actual, wtDir, err := SwitchChatBranch(mainDir, dataDir, sessionID, curBranch, false)
	if err != nil || wtDir == "" {
		_, _ = execGit(mainDir, "checkout", "-q", curBranch)
		return curBranch, "", false
	}
	return actual, wtDir, true
}

// CleanupChatWorktree removes a chat's separate folder copy when it has no
// unsaved file changes. The Git branch and its commits stay in the repository.
func CleanupChatWorktree(mainDir, workDir string) error {
	if workDir == "" {
		return nil
	}
	mainAbs, err := filepath.Abs(mainDir)
	if err == nil {
		if wtAbs, err := filepath.Abs(workDir); err == nil && wtAbs == mainAbs {
			return nil
		}
	}
	info, err := os.Stat(workDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.IsDir() {
		return err
	}
	status, err := execGit(workDir, "status", "--porcelain")
	if err == nil && strings.TrimSpace(status) != "" {
		return ErrDirtyWorktree
	}
	adminDir := findGitDir(workDir)
	if err := os.RemoveAll(workDir); err != nil {
		return err
	}
	if adminDir != "" && strings.Contains(filepath.ToSlash(adminDir), "/worktrees/") {
		_ = os.RemoveAll(adminDir)
	}
	return nil
}

func hasWorktree(dir string) bool {
	if dir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

func cleanStaleWorktreeAdmin(mainDir, sessionID, wtDir string) {
	gitDir := findGitDir(mainDir)
	if gitDir == "" {
		return
	}
	for _, name := range []string{sessionID, filepath.Base(wtDir)} {
		admin := filepath.Join(gitDir, "worktrees", name)
		gitdirFile := filepath.Join(admin, "gitdir")
		data, err := os.ReadFile(gitdirFile)
		if err != nil {
			continue
		}
		target := strings.TrimSpace(string(data))
		if target != "" {
			if _, statErr := os.Stat(target); errors.Is(statErr, os.ErrNotExist) {
				_ = os.RemoveAll(admin)
			}
		}
	}
}

func execGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			return "", err
		}
		return "", fmt.Errorf("%s", msg)
	}
	return stdout.String(), nil
}
