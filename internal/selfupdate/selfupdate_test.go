package selfupdate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t", "GIT_CONFIG_GLOBAL=/dev/null")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}

// forkFixture makes an upstream repo with release v1.1.0, a published fork
// based on v1.0.0, and a checkout of the fork with upstream as "origin".
func forkFixture(t *testing.T, forkContent string) (checkout string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@t")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@t")
	root := t.TempDir()
	upstream := filepath.Join(root, "upstream")
	require.NoError(t, os.Mkdir(upstream, 0o755))
	gitIn(t, upstream, "init", "-q", "-b", "main")
	write(t, upstream, "go.mod", module+"\n")
	write(t, upstream, "app.txt", "one\ntwo\nthree\nfour\nfive\n")
	gitIn(t, upstream, "add", ".")
	gitIn(t, upstream, "commit", "-qm", "v1.0.0")
	gitIn(t, upstream, "tag", "v1.0.0")

	fork := filepath.Join(root, "fork.git")
	gitIn(t, root, "clone", "-q", "--bare", upstream, fork)
	checkout = filepath.Join(root, "checkout")
	gitIn(t, root, "clone", "-q", fork, checkout)
	gitIn(t, checkout, "remote", "rename", "origin", "fork")
	gitIn(t, checkout, "remote", "add", "origin", upstream)
	gitIn(t, checkout, "branch", "-q", "--set-upstream-to=fork/main")
	// Route the real upstream URL to this local release fixture.
	gitIn(t, checkout, "config", "url."+upstream+".insteadOf", upstreamURL)
	write(t, checkout, "app.txt", forkContent)
	gitIn(t, checkout, "commit", "-qam", "fork change")
	gitIn(t, checkout, "push", "-q", "fork", "main")

	write(t, upstream, "app.txt", "one\nupstream two\nthree\nfour\nfive\n")
	write(t, upstream, "new.txt", "new\n")
	gitIn(t, upstream, "add", ".")
	gitIn(t, upstream, "commit", "-qm", "v1.1.0")
	gitIn(t, upstream, "tag", "v1.1.0")
	return checkout
}

func TestMergeKeepsTheForkAndAddsTheRelease(t *testing.T) {
	// The fork changed another line, so upstream's change merges cleanly.
	checkout := forkFixture(t, "one\ntwo\nthree\nfour\nfork five\n")
	write(t, checkout, "fork.txt", "fork only\n")
	gitIn(t, checkout, "add", ".")
	gitIn(t, checkout, "commit", "-qm", "fork feature")
	clashes, err := Merge(t.Context(), checkout, "v1.1.0")
	require.NoError(t, err)
	require.Empty(t, clashes)
	require.FileExists(t, filepath.Join(checkout, "fork.txt"))
	require.FileExists(t, filepath.Join(checkout, "new.txt"))
	data, err := os.ReadFile(filepath.Join(checkout, "app.txt"))
	require.NoError(t, err)
	require.Equal(t, "one\nupstream two\nthree\nfour\nfork five\n", string(data))
	require.Contains(t, gitIn(t, checkout, "log", "-1", "--format=%s"), "Merge upstream Crush v1.1.0")

	clashes, err = Merge(t.Context(), checkout, "v1.1.0")
	require.NoError(t, err)
	require.Empty(t, clashes, "an already merged release is not merged again")
}

func TestMergeReportsClashesAndLeavesThemForTheAgent(t *testing.T) {
	checkout := forkFixture(t, "one\nfork two\nthree\nfour\nfive\n")
	clashes, err := Merge(t.Context(), checkout, "v1.1.0")
	require.NoError(t, err)
	require.Equal(t, []string{"app.txt"}, clashes)
	require.True(t, MergeInProgress(t.Context(), checkout))
	clashes, err = Merge(t.Context(), checkout, "v1.1.0")
	require.NoError(t, err)
	require.Equal(t, []string{"app.txt"}, clashes, "a later run picks up the unfinished merge")
	require.ErrorContains(t, Build(t.Context(), checkout, "v1.1.0", os.Stderr), "unresolved clashes")
}

func TestMergeCommitsLocalChangesBeforeUpdating(t *testing.T) {
	checkout := forkFixture(t, "one\ntwo\nthree\nfour\nfork five\n")
	write(t, checkout, ".gitignore", "ignored.txt\n")
	write(t, checkout, "removed.txt", "old\n")
	gitIn(t, checkout, "add", "-A")
	gitIn(t, checkout, "commit", "-qm", "prepare local files")
	before := gitIn(t, checkout, "rev-parse", "HEAD")

	write(t, checkout, "app.txt", "one\ntwo\nthree\nfour\nlocal five\n")
	write(t, checkout, "staged.txt", "staged\n")
	gitIn(t, checkout, "add", "staged.txt")
	write(t, checkout, "staged.txt", "latest edit\n")
	write(t, checkout, "untracked.txt", "new local file\n")
	write(t, checkout, "ignored.txt", "keep ignored\n")
	require.NoError(t, os.Remove(filepath.Join(checkout, "removed.txt")))

	clashes, err := Merge(t.Context(), checkout, "v1.1.0")
	require.NoError(t, err)
	require.Empty(t, clashes)
	require.Equal(t, before, gitIn(t, checkout, "rev-parse", "HEAD^1^"))
	require.Equal(t, "Save local changes before updating Crush to v1.1.0", gitIn(t, checkout, "log", "-1", "--format=%s", "HEAD^1"))
	require.Equal(t, "one\ntwo\nthree\nfour\nlocal five", gitIn(t, checkout, "show", "HEAD^1:app.txt"))
	require.Equal(t, "latest edit", gitIn(t, checkout, "show", "HEAD^1:staged.txt"))
	require.Equal(t, "new local file", gitIn(t, checkout, "show", "HEAD^1:untracked.txt"))
	require.Empty(t, gitIn(t, checkout, "ls-tree", "--name-only", "HEAD^1", "removed.txt", "ignored.txt"))
	require.FileExists(t, filepath.Join(checkout, "ignored.txt"))
	require.Equal(t, "one\nupstream two\nthree\nfour\nlocal five", gitIn(t, checkout, "show", "HEAD:app.txt"))
	require.FileExists(t, filepath.Join(checkout, "new.txt"))
	require.Empty(t, gitIn(t, checkout, "status", "--porcelain"))
	after := gitIn(t, checkout, "rev-parse", "HEAD")
	clashes, err = Merge(t.Context(), checkout, "v1.1.0")
	require.NoError(t, err)
	require.Empty(t, clashes)
	require.Equal(t, after, gitIn(t, checkout, "rev-parse", "HEAD"), "a clean retry must not create another commit")
}

func TestMergeStopsWhenLocalCommitIsRejected(t *testing.T) {
	checkout := forkFixture(t, "one\ntwo\nthree\nfour\nfork five\n")
	head := gitIn(t, checkout, "rev-parse", "HEAD")
	write(t, checkout, "local.txt", "preserve this work\n")
	require.NoError(t, os.WriteFile(filepath.Join(checkout, ".git", "hooks", "pre-commit"), []byte("#!/bin/sh\necho 'commit rejected' >&2\nexit 1\n"), 0o755))
	_, err := Merge(t.Context(), checkout, "v1.1.0")
	require.ErrorContains(t, err, "cannot commit local changes")
	require.ErrorContains(t, err, "commit rejected")
	require.Equal(t, head, gitIn(t, checkout, "rev-parse", "HEAD"))
	require.Equal(t, "preserve this work", gitIn(t, checkout, "show", ":local.txt"))
	require.NoFileExists(t, filepath.Join(checkout, "new.txt"))
	require.False(t, MergeInProgress(t.Context(), checkout))
}

func TestMergedTagUsesStableCheckoutRelease(t *testing.T) {
	checkout := forkFixture(t, "one\ntwo\nthree\nfour\nfork five\n")
	_, err := Merge(t.Context(), checkout, "v1.1.0")
	require.NoError(t, err)
	gitIn(t, checkout, "tag", "v9.0.0-rc.1")
	tag, err := MergedTag(t.Context(), checkout)
	require.NoError(t, err)
	require.Equal(t, "v1.1.0", tag)
}

func TestMergePreservesDivergedHistory(t *testing.T) {
	checkout := forkFixture(t, "one\ntwo\nthree\nfour\nfork five\n")
	root := filepath.Dir(checkout)
	publisher := filepath.Join(root, "publisher")
	gitIn(t, root, "clone", "-q", filepath.Join(root, "fork.git"), publisher)
	write(t, publisher, "published.txt", "published work\n")
	gitIn(t, publisher, "add", ".")
	gitIn(t, publisher, "commit", "-qm", "published work")
	gitIn(t, publisher, "push", "-q")

	write(t, checkout, "local.txt", "local work\n")
	gitIn(t, checkout, "add", ".")
	gitIn(t, checkout, "commit", "-qm", "local work")
	head := gitIn(t, checkout, "rev-parse", "HEAD")
	published := gitIn(t, publisher, "rev-parse", "HEAD")
	clashes, err := Merge(t.Context(), checkout, "v1.1.0")
	require.NoError(t, err)
	require.Empty(t, clashes)
	gitIn(t, checkout, "merge-base", "--is-ancestor", head, "HEAD")
	gitIn(t, checkout, "merge-base", "--is-ancestor", published, "HEAD")
	require.Empty(t, gitIn(t, checkout, "status", "--porcelain"))
	require.FileExists(t, filepath.Join(checkout, "local.txt"))
	require.FileExists(t, filepath.Join(checkout, "published.txt"))
}

func TestFixPromptNamesTheWork(t *testing.T) {
	prompt := FixPrompt("v1.1.0", []string{"a.go", "b.go"}, "")
	require.Contains(t, prompt, "clashes in: a.go, b.go")
	require.Contains(t, prompt, "crush self-update build")
	require.Contains(t, prompt, "Do not install or push")
	prompt = FixPrompt("v1.1.0", nil, "--- FAIL: TestX")
	require.Contains(t, prompt, "--- FAIL: TestX")
}

func TestCreateLogMakesItsFolder(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "fresh"))
	log, err := CreateLog()
	require.NoError(t, err, "the first update ever must not fail on a missing folder")
	require.NoError(t, log.Close())
	require.FileExists(t, LogPath())
}

func TestMergeWithOriginPointingToFork(t *testing.T) {
	checkout := forkFixture(t, "one\ntwo\nthree\nfour\nfork five\n")
	gitIn(t, checkout, "remote", "set-url", "origin", filepath.Join(filepath.Dir(checkout), "fork.git"))
	gitIn(t, checkout, "fetch", "-q", "origin", "main")
	gitIn(t, checkout, "branch", "-q", "--set-upstream-to=origin/main")
	clashes, err := Merge(t.Context(), checkout, "v1.1.0")
	require.NoError(t, err)
	require.Empty(t, clashes)
	require.Equal(t, "origin/main", gitIn(t, checkout, "rev-parse", "--abbrev-ref", "@{upstream}"))
	require.FileExists(t, filepath.Join(checkout, "new.txt"))
	// Publishing still goes to the fork, not to the release source.
	gitIn(t, checkout, "push", "-q")
	require.Equal(t, gitIn(t, checkout, "rev-parse", "HEAD"), gitIn(t, filepath.Join(filepath.Dir(checkout), "fork.git"), "rev-parse", "main"))
	require.NotEqual(t, gitIn(t, checkout, "rev-parse", "HEAD"), gitIn(t, filepath.Join(filepath.Dir(checkout), "upstream"), "rev-parse", "main"))
}

func TestTestCommandWithoutSystemdOrGNUTimeout(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	require.Equal(t, []string{"go", "test", "./..."}, testCommand(t.TempDir()))
}

func TestStagedBuildRequiresCommittedChanges(t *testing.T) {
	checkout := forkFixture(t, "one\ntwo\nthree\nfour\nfork five\n")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir, err := updateDir()
	require.NoError(t, err)
	binary := filepath.Join(dir, "crush")
	require.NoError(t, os.WriteFile(binary, []byte("tested build"), 0o755))
	require.NoError(t, saveState(state{
		Tag: "v1.0.0", Version: "v1.0.0-local-test", Binary: binary,
		Head: gitIn(t, checkout, "rev-parse", "HEAD"),
	}))
	require.Equal(t, "v1.0.0-local-test", Staged(t.Context(), checkout, "v1.0.0"))
	write(t, checkout, "local.txt", "new work after the build\n")
	require.Empty(t, Staged(t.Context(), checkout, "v1.0.0"), "new changes must go through the commit and build steps")
	_, _, err = Install(t.Context(), checkout)
	require.ErrorContains(t, err, "checkout changed")
}

func TestMergeSavesChangesWhenPublishedForkIsAhead(t *testing.T) {
	checkout := forkFixture(t, "one\ntwo\nthree\nfour\nfork five\n")
	root := filepath.Dir(checkout)
	publisher := filepath.Join(root, "publisher")
	gitIn(t, root, "clone", "-q", filepath.Join(root, "fork.git"), publisher)
	write(t, publisher, "published.txt", "from another machine\n")
	gitIn(t, publisher, "add", "-A")
	gitIn(t, publisher, "commit", "-qm", "published change")
	gitIn(t, publisher, "push", "-q")
	write(t, checkout, "local.txt", "uncommitted work\n")
	clashes, err := Merge(t.Context(), checkout, "v1.1.0")
	require.NoError(t, err)
	require.Empty(t, clashes)
	require.Equal(t, "uncommitted work", gitIn(t, checkout, "show", "HEAD:local.txt"))
	require.Equal(t, "from another machine", gitIn(t, checkout, "show", "HEAD:published.txt"))
	gitIn(t, checkout, "merge-base", "--is-ancestor", "v1.1.0", "HEAD")
	require.Empty(t, gitIn(t, checkout, "status", "--porcelain"))
}

func TestMergeReportsPublishedForkClashesBeforeRelease(t *testing.T) {
	checkout := forkFixture(t, "one\ntwo\nthree\nfour\nfork five\n")
	root := filepath.Dir(checkout)
	publisher := filepath.Join(root, "publisher")
	gitIn(t, root, "clone", "-q", filepath.Join(root, "fork.git"), publisher)
	write(t, publisher, "local.txt", "published work\n")
	gitIn(t, publisher, "add", "-A")
	gitIn(t, publisher, "commit", "-qm", "published change")
	gitIn(t, publisher, "push", "-q")
	write(t, checkout, "local.txt", "uncommitted work\n")
	clashes, err := Merge(t.Context(), checkout, "v1.1.0")
	require.NoError(t, err)
	require.Equal(t, []string{"local.txt"}, clashes)
	require.True(t, MergeInProgress(t.Context(), checkout))
	require.Equal(t, "uncommitted work", gitIn(t, checkout, "show", "HEAD:local.txt"))
	gitIn(t, checkout, "rev-parse", "--verify", "v1.1.0")
}

func TestMergeRefusesOfficialUpstreamTracking(t *testing.T) {
	for _, remoteURL := range []string{upstreamURL, "git@github.com:charmbracelet/crush.git", "ssh://git@github.com/charmbracelet/crush.git"} {
		t.Run(remoteURL, func(t *testing.T) {
			checkout := forkFixture(t, "one\ntwo\nthree\nfour\nfork five\n")
			gitIn(t, checkout, "remote", "set-url", "fork", remoteURL)
			head := gitIn(t, checkout, "rev-parse", "HEAD")
			write(t, checkout, "local.txt", "uncommitted work\n")
			_, err := Merge(t.Context(), checkout, "v1.1.0")
			require.ErrorContains(t, err, "tracks the official Crush repository")
			require.Equal(t, head, gitIn(t, checkout, "rev-parse", "HEAD"))
			require.Equal(t, "?? local.txt", gitIn(t, checkout, "status", "--porcelain"))
		})
	}
}
