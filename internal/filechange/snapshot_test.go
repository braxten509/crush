package filechange

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func put(t *testing.T, root, path, content string) {
	t.Helper()
	path = filepath.Join(root, path)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func byPath(review *Review) map[string]Change {
	result := map[string]Change{}
	for _, change := range review.Changes {
		result[filepath.Base(change.Path)] = change
	}
	return result
}

func TestExplicitFileChangesWithoutGit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a POSIX shell")
	}
	t.Parallel()
	root := t.TempDir()
	put(t, root, "edit.txt", "before\n")
	put(t, root, "gone.txt", "deleted\n")
	put(t, root, "old.txt", "moved\n")
	put(t, root, "empty.txt", "")
	put(t, root, ".gitignore", "ignored/\n")
	put(t, root, "ignored/item.txt", "old ignored\n")
	tracker, err := New(t.Context(), root)
	require.NoError(t, err)
	for _, name := range []string{"edit.txt", "gone.txt", "old.txt", "empty.txt", "created.txt", "new-empty.txt", "new.txt", "ignored/item.txt", ".hidden"} {
		tracker.Track(name)
	}
	cmd := exec.CommandContext(t.Context(), "sh", "-c", `
sed -i 's/before/after/' edit.txt
mv old.txt new.txt
rm gone.txt empty.txt
printf 'created\n' > created.txt
: > new-empty.txt
printf 'new ignored\n' > ignored/item.txt
printf 'secret-free fixture\n' > .hidden
chmod 755 edit.txt
exit 7`)
	cmd.Dir = root
	require.Error(t, cmd.Run(), "partial work must survive a failing command")
	review, err := tracker.Checkpoint(t.Context())
	require.NoError(t, err)
	changes := byPath(review)
	require.Len(t, changes, 9)
	require.Equal(t, "before\n", changes["edit.txt"].Before.Content)
	require.Equal(t, "after\n", changes["edit.txt"].After.Content)
	require.EqualValues(t, 0o755, changes["edit.txt"].After.Mode)
	require.Nil(t, changes["gone.txt"].After)
	require.Nil(t, changes["empty.txt"].After)
	require.NotNil(t, changes["empty.txt"].Before)
	require.Nil(t, changes["old.txt"].After)
	require.Nil(t, changes["new.txt"].Before)
	require.Equal(t, "moved\n", changes["new.txt"].After.Content)
	require.NotNil(t, changes["new-empty.txt"].After)
	require.Equal(t, "new ignored\n", changes["item.txt"].After.Content)
	require.Equal(t, "secret-free fixture\n", changes[".hidden"].After.Content)

	// Every section starts from the previous checkpoint, and old reviews
	// retain their captured content after later sections edit the same file.
	put(t, root, "edit.txt", "next section\n")
	next, err := tracker.Checkpoint(t.Context())
	require.NoError(t, err)
	require.Len(t, next.Changes, 1)
	require.Equal(t, "after\n", next.Changes[0].Before.Content)
	require.Equal(t, "after\n", changes["edit.txt"].After.Content)
	noChanges, err := tracker.Checkpoint(t.Context())
	require.NoError(t, err)
	require.Empty(t, noChanges.Changes)
}

func TestIgnoredBookkeepingAndBinaryChanges(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	put(t, root, ".git/index", "git metadata")
	put(t, root, "runtime/state.db", "crush data")
	put(t, root, "runtime-source.txt", "not excluded")
	put(t, root, "image.bin", "\x00\x01old")
	put(t, root, "large.txt", strings.Repeat("x", maxTextSize+1))
	tracker, err := New(t.Context(), root, filepath.Join(root, "runtime"))
	require.NoError(t, err)
	for _, name := range []string{".git/index", "runtime/state.db", "runtime-source.txt", "image.bin", "large.txt"} {
		tracker.Track(name)
	}
	put(t, root, ".git/index", "updated metadata")
	put(t, root, "runtime/state.db", "updated state")
	put(t, root, "runtime-source.txt", "included")
	put(t, root, "image.bin", "\x00\x01new")
	put(t, root, "large.txt", strings.Repeat("y", maxTextSize+2))
	review, err := tracker.Checkpoint(t.Context())
	require.NoError(t, err)
	changes := byPath(review)
	require.Len(t, changes, 3)
	require.Equal(t, "Binary file", changes["image.bin"].Before.Omitted)
	require.Empty(t, changes["image.bin"].Before.Content)
	require.NotEqual(t, changes["image.bin"].Before.Digest, changes["image.bin"].After.Digest)
	require.Contains(t, changes["large.txt"].After.Omitted, "preview limit")
	require.NotEmpty(t, changes["runtime-source.txt"].After.Content)
}

func TestCancelledScanKeepsBaseline(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	put(t, root, "file", "original")
	tracker, err := New(t.Context(), root)
	require.NoError(t, err)
	tracker.Track("file")
	put(t, root, "file", "new")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = tracker.Checkpoint(ctx)
	require.ErrorIs(t, err, context.Canceled)
	review, err := tracker.Checkpoint(t.Context())
	require.NoError(t, err)
	require.Equal(t, "original", review.Changes[0].Before.Content)
}

func TestExplicitExternalFileAndSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires symlinks")
	}
	t.Parallel()
	root, outside := t.TempDir(), t.TempDir()
	put(t, outside, "outside.txt", "before")
	put(t, outside, "not-tracked.txt", "private fixture")
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "link")))
	tracker, err := New(t.Context(), root)
	require.NoError(t, err)
	tracker.Track("link")
	tracker.Track(filepath.Join(outside, "outside.txt"))
	put(t, outside, "through-link.txt", "linked before")
	tracker.Track(filepath.Join(root, "link", "through-link.txt"))
	put(t, outside, "through-link.txt", "linked after")
	put(t, outside, "outside.txt", "after")
	put(t, outside, "not-tracked.txt", "not followed")
	require.NoError(t, os.Remove(filepath.Join(root, "link")))
	require.NoError(t, os.Symlink(filepath.Join(outside, "outside.txt"), filepath.Join(root, "link")))
	review, err := tracker.Checkpoint(t.Context())
	require.NoError(t, err)
	changes := byPath(review)
	require.Len(t, changes, 3)
	require.Equal(t, "linked before", changes["through-link.txt"].Before.Content)
	require.Equal(t, "linked after", changes["through-link.txt"].After.Content)
	require.Equal(t, "before", changes["outside.txt"].Before.Content)
	require.Equal(t, outside, changes["link"].Before.Content)
	require.Equal(t, filepath.Join(outside, "outside.txt"), changes["link"].After.Content)
}

func TestRestoredMtimeDoesNotHideEdits(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("ctime support is platform-specific")
	}
	t.Parallel()
	root := t.TempDir()
	put(t, root, "file", "before")
	info, err := os.Stat(filepath.Join(root, "file"))
	require.NoError(t, err)
	tracker, err := New(t.Context(), root)
	require.NoError(t, err)
	tracker.Track("file")
	put(t, root, "file", "AFTER!")
	require.NoError(t, os.Chtimes(filepath.Join(root, "file"), info.ModTime(), info.ModTime()))
	review, err := tracker.Checkpoint(t.Context())
	require.NoError(t, err)
	require.Len(t, review.Changes, 1)
}

func BenchmarkCheckpointUnchanged(b *testing.B) {
	root := b.TempDir()
	for i := range 2000 {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("file-%04d", i)), []byte(strings.Repeat("source line\n", 100)), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	tracker, err := New(b.Context(), root)
	if err != nil {
		b.Fatal(err)
	}
	tracker.Track("file-0000")
	b.ResetTimer()
	for b.Loop() {
		if _, err := tracker.Checkpoint(b.Context()); err != nil {
			b.Fatal(err)
		}
	}
}

// Starting in a home folder must not discover its projects, caches or files.
func TestOnlyExplicitFilesAreReviewed(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	put(t, root, "named.txt", "before")
	put(t, root, "unrelated.txt", "unrelated")
	put(t, root, "subfolder/hidden.txt", "hidden")
	tracker, err := New(t.Context(), root)
	require.NoError(t, err)
	require.Empty(t, tracker.files)
	require.False(t, tracker.Contains("named.txt"))
	tracker.Track("subfolder") // A directory argument never expands to files.
	tracker.Track("named.txt")
	require.Len(t, tracker.files, 1)
	require.True(t, tracker.Contains("named.txt"))
	require.False(t, tracker.Contains("subfolder/hidden.txt"))
	put(t, root, "named.txt", "after")
	put(t, root, "unrelated.txt", "changed elsewhere")
	put(t, root, "subfolder/hidden.txt", "changed elsewhere")
	put(t, root, "unknown-new.txt", "not reported by a tool")
	review, err := tracker.Checkpoint(t.Context())
	require.NoError(t, err)
	require.Len(t, review.Changes, 1)
	require.Equal(t, filepath.Join(root, "named.txt"), review.Changes[0].Path)
	require.Len(t, tracker.files, 1)
}
