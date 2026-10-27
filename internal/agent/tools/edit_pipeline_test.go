package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/history"
	"github.com/stretchr/testify/require"
)

// versionLog is a history service that keeps every version it is given.
type versionLog struct {
	mockHistoryService
	latest   map[string]string
	versions []string
}

func (h *versionLog) Create(_ context.Context, _, path, content string) (history.File, error) {
	h.latest[path] = content
	h.versions = append(h.versions, content)
	return history.File{Path: path, Content: content}, nil
}

func (h *versionLog) CreateVersion(_ context.Context, _, path, content string) (history.File, error) {
	h.latest[path] = content
	h.versions = append(h.versions, content)
	return history.File{Path: path, Content: content}, nil
}

func (h *versionLog) GetByPathAndSession(_ context.Context, path, _ string) (history.File, error) {
	content, ok := h.latest[path]
	if !ok {
		return history.File{}, errors.New("not found")
	}
	return history.File{Path: path, Content: content}, nil
}

func newRepoFile(t *testing.T, content string) (root, path string) {
	t.Helper()
	root = t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, ".git"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644))
	path = filepath.Join(root, "file.txt")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return root, path
}

func TestRecordFileChangeFirstTouchRecordsOldAndNewOnly(t *testing.T) {
	t.Parallel()

	root, path := newRepoFile(t, "before")
	versions := &versionLog{latest: map[string]string{}}
	tracker := &mockEditFileTracker{}
	edit := editContext{ctx: t.Context(), workingDir: root, files: versions, filetracker: tracker}

	require.NoError(t, commitFileChange(edit, "session", path, "before", "after"))
	// A file the history has never seen gets its old version and its new
	// one, with no duplicate of the old version in between.
	require.Equal(t, []string{"before", "after"}, versions.versions)
	require.Equal(t, []string{path}, tracker.reads)
}

func TestRecordFileChangeKeepsOutsideChangesAsIntermediateVersion(t *testing.T) {
	t.Parallel()

	root, path := newRepoFile(t, "changed outside")
	versions := &versionLog{latest: map[string]string{path: "last recorded"}}
	edit := editContext{ctx: t.Context(), workingDir: root, files: versions, filetracker: &mockEditFileTracker{}}

	require.NoError(t, commitFileChange(edit, "session", path, "changed outside", "after"))
	require.Equal(t, []string{"changed outside", "after"}, versions.versions)

	versions.versions = nil
	require.NoError(t, commitFileChange(edit, "session", path, "after", "again"))
	require.Equal(t, []string{"again"}, versions.versions)
}

func TestRecordFileChangeToleratesMissingServices(t *testing.T) {
	t.Parallel()

	root, path := newRepoFile(t, "before")
	edit := editContext{ctx: t.Context(), workingDir: root}
	require.NoError(t, recordFileChange(edit, "session", path, "before", "after"))
}

func TestCheckFreshWording(t *testing.T) {
	t.Parallel()

	_, path := newRepoFile(t, "content")
	info, err := os.Stat(path)
	require.NoError(t, err)
	ctx := t.Context()

	never := &mockEditFileTracker{}
	require.Equal(t, "you must read the file before editing it. Use the View tool first",
		checkFresh(ctx, never, "s", path, info, staleEdit))
	require.Contains(t, checkFresh(ctx, never, "s", path, info, staleWrite),
		"has been modified since it was last read.\nLast modification:")

	stale := &mockEditFileTracker{lastRead: info.ModTime().Add(-time.Hour)}
	require.Contains(t, checkFresh(ctx, stale, "s", path, info, staleEdit),
		"has been modified since it was last read (mod time:")

	fresh := &mockEditFileTracker{lastRead: info.ModTime().Add(time.Second)}
	require.Empty(t, checkFresh(ctx, fresh, "s", path, info, staleEdit))
	require.Empty(t, checkFresh(ctx, fresh, "s", path, info, staleWrite))
}
