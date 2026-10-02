//go:build linux && amd64

package filechange

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func runObserved(t *testing.T, root, command string) (*Review, error) {
	t.Helper()
	return runObservedScript(t, root, command, command)
}

// runObservedScript reports command as the tool call but runs script, the way
// a CLI runs its call inside its own wrapper.
func runObservedScript(t *testing.T, root, command, script string) (*Review, error) {
	t.Helper()
	// The fake agent starts a shell only after its tool call has been reported.
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", `read -r _; /bin/sh -c "$1"; code=$?; exit "$code"`, "fixture", script)
	cmd.Dir = root
	input, err := cmd.StdinPipe()
	require.NoError(t, err)
	cmd.Stdout = io.Discard
	cmd.Stderr = os.Stderr
	monitor, err := StartProcess(cmd, root)
	require.NoError(t, err)
	monitor.Begin("bash", command)
	_, err = io.WriteString(input, "go\n")
	require.NoError(t, err)
	_ = input.Close()
	err = monitor.Wait(cmd)
	return monitor.End("bash"), err
}

func TestShellReviewDynamicPathsOutsideWorkspace(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	put(t, outside, "hello.txt", "existing\n")
	program := fmt.Sprintf(`import pathlib
root = pathlib.Path(%q)
for index in range(1000):
 name = 'hello.txt' if index == 0 else f'hello-{index}.txt'
 try:
  with (root / name).open('x', encoding='utf-8') as file:
   file.write('hello\n')
  break
 except FileExistsError:
  continue
`, outside)
	command := "python3 - <<'PY'\n" + program + "PY"
	review, err := runObserved(t, root, command)
	require.NoError(t, err)
	require.NotNil(t, review)
	require.Len(t, review.Changes, 1)
	require.Equal(t, filepath.Join(outside, "hello-1.txt"), review.Changes[0].Path)
	require.Nil(t, review.Changes[0].Before)
	require.Equal(t, "hello\n", review.Changes[0].After.Content)
}

func TestShellReviewCapturesOverwriteRenameDeleteAndPartialFailure(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"overwrite", "rename", "delete", "unrelated"} {
		put(t, root, name, "before\n")
	}
	command := `python3 - <<'PY'
from pathlib import Path
Path('overwrite').write_text('after\n')
Path('rename').rename('renamed')
Path('delete').unlink()
raise SystemExit(7)
PY`
	review, err := runObserved(t, root, command)
	require.Error(t, err)
	require.NotNil(t, review)
	changes := byPath(review)
	require.Len(t, changes, 4)
	require.Equal(t, "before\n", changes["overwrite"].Before.Content)
	require.Equal(t, "after\n", changes["overwrite"].After.Content)
	require.Nil(t, changes["rename"].After)
	require.Nil(t, changes["renamed"].Before)
	require.Nil(t, changes["delete"].After)
}

// Claude Code runs each call as `eval '<quoted command>'` inside a wrapper
// script, so builtins such as redirections write from the wrapper shell itself.
func TestShellReviewMatchesCommandQuotedInsideWrapper(t *testing.T) {
	for name, escape := range map[string]string{"claude": `'"'"'`, "posix": `'\''`} {
		t.Run(name, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			target := filepath.Join(outside, "Secure entries.txt")
			command := fmt.Sprintf(`ls -d %q && [ -e %q ] || echo "it's here" > %q`, outside, target, target)
			quoted := "'" + strings.ReplaceAll(command, "'", escape) + "'"
			review, err := runObservedScript(t, root, command, "true && eval "+quoted+" < /dev/null && pwd -P >/dev/null")
			require.NoError(t, err)
			require.NotNil(t, review)
			require.Len(t, review.Changes, 1)
			require.Equal(t, target, review.Changes[0].Path)
			require.Equal(t, "it's here\n", review.Changes[0].After.Content)
		})
	}
}

func TestShellReviewIgnoresWrapperOfAnotherCommand(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", `read -r _; /bin/sh -c "eval 'printf x > other'"`)
	cmd.Dir = root
	input, err := cmd.StdinPipe()
	require.NoError(t, err)
	monitor, err := StartProcess(cmd, root)
	require.NoError(t, err)
	monitor.Begin("bash", "printf x")
	_, _ = io.WriteString(input, "go\n")
	_ = input.Close()
	require.NoError(t, monitor.Wait(cmd))
	require.Nil(t, monitor.End("bash"))
}

func TestReadOnlyShellHasNoReview(t *testing.T) {
	root := t.TempDir()
	put(t, root, "read.txt", "unchanged\n")
	review, err := runObserved(t, root, "cat read.txt")
	require.NoError(t, err)
	require.Nil(t, review)
}

func TestReviewMetadataRoundTrip(t *testing.T) {
	original := `{"output":"hello"}`
	review := &Review{Root: "/tmp", Changes: []Change{{Path: "/tmp/file", After: &State{Content: "hello"}}}}
	metadata, decoded := TakeReview(WithReview(original, review))
	require.Equal(t, review, decoded)
	require.JSONEq(t, original, metadata)
	_, err := json.Marshal(decoded)
	require.NoError(t, err)
}

func TestShellReviewsKeepParallelProcessesSeparate(t *testing.T) {
	for index := range 6 {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			t.Parallel()
			review, err := runObserved(t, t.TempDir(), fmt.Sprintf("printf 'result-%d' > result", index))
			require.NoError(t, err)
			require.NotNil(t, review)
			require.Len(t, review.Changes, 1)
			require.Equal(t, fmt.Sprintf("result-%d", index), review.Changes[0].After.Content)
		})
	}
}

func TestShellReviewPreservesExitStatus(t *testing.T) {
	for _, code := range []int{0, 3, 17} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			_, err := runObserved(t, t.TempDir(), fmt.Sprintf("exit %d", code))
			if code == 0 {
				require.NoError(t, err)
			} else {
				var exit *exec.ExitError
				require.ErrorAs(t, err, &exit)
				require.Equal(t, code, exit.ExitCode())
			}
		})
	}
}
