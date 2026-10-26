//go:build linux && amd64

package filechange

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

// Git deletes a file before writing its new version, and deletions are not
// watched, so the old version comes from Git. A truly new file stays new.
func TestGitMergeKeepsOldVersionAsBaseline(t *testing.T) {
	root := visibleRestoreRoot(t)
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	git("init", "-q")
	put(t, root, "kept.txt", "one\ntwo\nthree\n")
	git("add", "-A")
	git("commit", "-qm", "base")
	git("checkout", "-qb", "side")
	put(t, root, "kept.txt", "one\n2\nthree\n")
	put(t, root, "new.txt", "fresh\n")
	git("add", "-A")
	git("commit", "-qm", "side")
	git("checkout", "-q", "-")

	review, err := runObserved(t, root, "git -c user.name=t -c user.email=t@t -c commit.gpgsign=false merge -q --no-ff side -m merged")
	require.NoError(t, err)
	require.NotNil(t, review)
	changes := byPath(review)
	require.NotNil(t, changes["kept.txt"].Before)
	require.Equal(t, "one\ntwo\nthree\n", changes["kept.txt"].Before.Content)
	require.NotEmpty(t, changes["kept.txt"].Before.RestoreData)
	require.Equal(t, "one\n2\nthree\n", changes["kept.txt"].After.Content)
	require.Nil(t, changes["new.txt"].Before)
}
