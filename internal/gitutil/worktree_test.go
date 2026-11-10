package gitutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	run("init", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644))
	run("add", "README.md")
	run("commit", "-m", "initial")
	return dir
}

func TestTwoChatsSeparateBranchesSimultaneously(t *testing.T) {
	t.Parallel()
	mainDir := initTestRepo(t)
	dataDir := filepath.Join(mainDir, ".crush")

	// Chat 1 creates and switches to branch "feat-one".
	b1, wt1, err := SwitchChatBranch(mainDir, dataDir, "chat-1", "feat-one", true)
	require.NoError(t, err)
	require.Equal(t, "feat-one", b1)
	require.NotEmpty(t, wt1)
	require.NotEqual(t, mainDir, wt1)

	// Chat 2 creates and switches to branch "feat-two".
	b2, wt2, err := SwitchChatBranch(mainDir, dataDir, "chat-2", "feat-two", true)
	require.NoError(t, err)
	require.Equal(t, "feat-two", b2)
	require.NotEmpty(t, wt2)
	require.NotEqual(t, mainDir, wt2)
	require.NotEqual(t, wt1, wt2)

	// Main folder remains on "main".
	require.Equal(t, "main", CurrentBranch(mainDir))
	require.Equal(t, "feat-one", CurrentBranch(wt1))
	require.Equal(t, "feat-two", CurrentBranch(wt2))

	// Both chats edit the same file on their own branches at the same time.
	require.NoError(t, os.WriteFile(filepath.Join(wt1, "README.md"), []byte("from chat 1\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(wt2, "README.md"), []byte("from chat 2\n"), 0o644))

	mainBytes, err := os.ReadFile(filepath.Join(mainDir, "README.md"))
	require.NoError(t, err)
	require.Equal(t, "hello\n", string(mainBytes))

	// Dirty worktree is not removed on close.
	require.ErrorIs(t, CleanupChatWorktree(mainDir, wt1), ErrDirtyWorktree)

	// Revert chat 1's unsaved edit so it is clean, then close it.
	require.NoError(t, os.WriteFile(filepath.Join(wt1, "README.md"), []byte("hello\n"), 0o644))
	require.NoError(t, CleanupChatWorktree(mainDir, wt1))
	_, err = os.Stat(wt1)
	require.True(t, os.IsNotExist(err))

	// Reopening chat 1 recreates its worktree on "feat-one" automatically.
	reopenedDir, reopenedBranch := EnsureChatWorktree(mainDir, dataDir, "chat-1", "feat-one", "")
	require.Equal(t, "feat-one", reopenedBranch)
	require.Equal(t, wt1, reopenedDir)
	require.Equal(t, "feat-one", CurrentBranch(reopenedDir))
}

func TestIsolateMainBranchSwitch(t *testing.T) {
	t.Parallel()
	mainDir := initTestRepo(t)
	dataDir := filepath.Join(mainDir, ".crush")

	// Simulate an agent switching branches directly in mainDir.
	cmd := exec.Command("git", "-C", mainDir, "checkout", "-b", "agent-branch")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Equal(t, "agent-branch", CurrentBranch(mainDir))

	// Isolate the switch to chat-1's worktree while restoring mainDir to "main".
	newBranch, wtDir, isolated := IsolateMainBranchSwitch(mainDir, dataDir, "chat-1", "main")
	require.True(t, isolated)
	require.Equal(t, "agent-branch", newBranch)
	require.NotEmpty(t, wtDir)
	require.Equal(t, "main", CurrentBranch(mainDir))
	require.Equal(t, "agent-branch", CurrentBranch(wtDir))
}
