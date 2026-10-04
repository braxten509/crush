package filehistory

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/stretchr/testify/require"
)

type fixture struct {
	store *Store
	q     *db.Queries
	conn  *sql.DB
	root  string
	count int64
}

func setup(t *testing.T) *fixture {
	t.Helper()
	data := t.TempDir()
	conn, err := db.Connect(t.Context(), data)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Release(data)) })
	// /tmp and cache paths are intentionally invisible to restores. Fixture
	// files live in a unique visible folder; no existing project file is used.
	root, err := os.MkdirTemp(".", "restore-test-")
	require.NoError(t, err)
	root, err = filepath.Abs(root)
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(root) })
	f := &fixture{store: New(conn, data), q: db.New(conn), conn: conn, root: root}
	_, err = f.q.CreateSession(t.Context(), db.CreateSessionParams{ID: "chat", Title: "test"})
	require.NoError(t, err)
	return f
}
func state(text string) *filechange.State {
	return &filechange.State{Content: text, Size: int64(len(text)), Mode: 0644}
}
func (f *fixture) change(t *testing.T, id, path string, before, after *filechange.State) {
	t.Helper()
	f.count++
	_, err := f.q.CreateMessage(t.Context(), db.CreateMessageParams{ID: id, SessionID: "chat", Role: "tool", Parts: "[]"})
	require.NoError(t, err)
	require.NoError(t, f.store.Capture(t.Context(), "chat", id, id, &filechange.Review{Root: f.root, Changes: []filechange.Change{{Path: path, Before: before, After: after, Order: f.count}}}))
}
func (f *fixture) write(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
}
func contents(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}
func TestSameBranchRestoreUndoAndRestart(t *testing.T) {
	f := setup(t)
	path := filepath.Join(f.root, "notes.txt")
	f.write(t, path, "second\n")
	f.change(t, "first", path, state("start\n"), state("first\n"))
	f.change(t, "second", path, state("first\n"), state("second\n"))
	plan, err := f.store.Preview(t.Context(), "chat", "first")
	require.NoError(t, err)
	require.Len(t, plan.Entries, 1)
	require.Empty(t, plan.Entries[0].Conflict)
	require.Equal(t, 1, plan.Entries[0].Adds)
	result, err := f.store.Apply(t.Context(), plan)
	require.NoError(t, err)
	require.Equal(t, 1, result.Restored)
	require.Equal(t, "first\n", contents(t, path))
	f.store = New(f.conn, filepath.Dir(filepath.Dir(f.store.directory)))
	undo, err := f.store.UndoPreview(t.Context(), "chat")
	require.NoError(t, err)
	_, err = f.store.Apply(t.Context(), undo)
	require.NoError(t, err)
	require.Equal(t, "second\n", contents(t, path))
}
func TestCrossBranchAndCreatedDeletedModes(t *testing.T) {
	f := setup(t)
	p := filepath.Join(f.root, "file")
	created := filepath.Join(f.root, "created")
	deleted := filepath.Join(f.root, "deleted")
	f.change(t, "root", p, state("zero"), state("root"))
	f.change(t, "old", p, state("root"), state("old"))
	f.change(t, "created", created, nil, state("created"))
	mode := state("deleted")
	mode.Mode = 0750
	f.change(t, "deleted", deleted, mode, nil)
	require.NoError(t, f.q.SwitchTree(t.Context(), "chat", "root"))
	f.change(t, "other", p, state("root"), state("other"))
	require.NoError(t, f.q.SwitchTree(t.Context(), "chat", "deleted"))
	f.write(t, p, "old")
	f.write(t, created, "created")
	plan, err := f.store.Preview(t.Context(), "chat", "other")
	require.NoError(t, err)
	require.Len(t, plan.Entries, 3)
	r, err := f.store.Apply(t.Context(), plan)
	require.NoError(t, err)
	require.Equal(t, 3, r.Restored)
	require.Equal(t, "other", contents(t, p))
	require.NoFileExists(t, created)
	require.Equal(t, "deleted", contents(t, deleted))
	info, err := os.Stat(deleted)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0750), info.Mode().Perm())
}
func TestConflictsSkippedAndRechecked(t *testing.T) {
	f := setup(t)
	p := filepath.Join(f.root, "file")
	q := filepath.Join(f.root, "second")
	f.change(t, "a", p, state("before"), state("after"))
	f.change(t, "b", q, state("old"), state("new"))
	f.write(t, p, "manual")
	f.write(t, q, "new")
	plan, err := f.store.Preview(t.Context(), "chat", "")
	require.NoError(t, err)
	r, err := f.store.Apply(t.Context(), plan)
	require.NoError(t, err)
	require.Equal(t, 1, r.Restored)
	require.Contains(t, r.Skipped, p)
	require.Equal(t, "manual", contents(t, p))
	require.Equal(t, "old", contents(t, q))
	f.write(t, p, "after")
	f.write(t, q, "new")
	plan, err = f.store.Preview(t.Context(), "chat", "")
	require.NoError(t, err)
	f.write(t, q, "later manual")
	r, err = f.store.Apply(t.Context(), plan)
	require.NoError(t, err)
	require.Contains(t, r.Skipped, q)
	require.Equal(t, "later manual", contents(t, q))
}
func TestTooLargeHiddenAndBinary(t *testing.T) {
	f := setup(t)
	p := filepath.Join(f.root, "large")
	large := state("")
	large.Size = filechange.MaxRestoreSize + 1
	large.Digest = "stat:large"
	f.change(t, "a", p, state("before"), large)
	plan, err := f.store.Preview(t.Context(), "chat", "")
	require.NoError(t, err)
	require.Contains(t, plan.Entries[0].Unavailable, "Too large")
	hidden := filepath.Join(f.root, ".hidden", "file")
	f.change(t, "hidden", hidden, nil, state("secret"))
	var n int
	require.NoError(t, f.conn.QueryRow(`SELECT count(*) FROM file_history_changes WHERE path=?`, hidden).Scan(&n))
	require.Zero(t, n)
	binary := []byte{0, 255, 2, 0}
	snapshot := &filechange.State{RestoreData: base64.StdEncoding.EncodeToString(binary), Omitted: "Binary file", Mode: 0644, Size: 4}
	p = filepath.Join(f.root, "binary")
	f.change(t, "binary", p, snapshot, state("now"))
	f.write(t, p, "now")
	plan, err = f.store.Preview(t.Context(), "chat", "hidden")
	require.NoError(t, err)
	_, err = f.store.Apply(t.Context(), plan)
	require.NoError(t, err)
	require.Equal(t, string(binary), contents(t, p))
}
func TestBudgetEvictionAndChatDeletionGC(t *testing.T) {
	f := setup(t)
	p := filepath.Join(f.root, "file")
	data := make([]byte, 4096)
	_, err := rand.Read(data)
	require.NoError(t, err)
	before := &filechange.State{Mode: 0644, Size: int64(len(data)), RestoreData: base64.StdEncoding.EncodeToString(data)}
	f.change(t, "a", p, before, state("after"))
	f.write(t, p, "after")
	// The existing object is deduplicated across before/after and tool flushes.
	require.NoError(t, f.store.Capture(t.Context(), "chat", "a", "a", &filechange.Review{Root: f.root, Changes: []filechange.Change{{Path: p, Before: before, After: state("after"), Order: 1}}}))
	var n int
	require.NoError(t, f.conn.QueryRow(`SELECT count(*) FROM file_history_objects`).Scan(&n))
	require.Equal(t, 2, n)
	f.store.Budget = 100
	require.NoError(t, f.store.Maintain(t.Context()))
	plan, err := f.store.Preview(t.Context(), "chat", "")
	require.NoError(t, err)
	require.Contains(t, plan.Entries[0].Unavailable, "2 GB")
	require.NoError(t, f.conn.QueryRow(`SELECT count(*) FROM file_history_objects`).Scan(&n))
	require.Zero(t, n)
	f.store.Budget = ProjectBudget
	f.change(t, "b", p, state("after"), state("latest"))
	require.NoError(t, f.q.DeleteSession(t.Context(), "chat"))
	require.NoError(t, f.store.Maintain(t.Context()))
	require.NoError(t, f.conn.QueryRow(`SELECT count(*) FROM file_history_objects`).Scan(&n))
	require.Zero(t, n)
	require.NoError(t, f.conn.QueryRow(`SELECT count(*) FROM file_history_changes`).Scan(&n))
	require.Zero(t, n)
}
func TestCopyKeepsIndependentHistory(t *testing.T) {
	f := setup(t)
	p := filepath.Join(f.root, "file")
	f.change(t, "a", p, state("before"), state("after"))
	f.write(t, p, "after")
	require.NoError(t, f.q.CopyTree(t.Context(), "chat", "a", "copy"))
	require.NoError(t, f.q.DeleteSession(t.Context(), "chat"))
	require.NoError(t, f.store.Maintain(t.Context()))
	plan, err := f.store.Preview(t.Context(), "copy", "")
	require.NoError(t, err)
	require.Len(t, plan.Entries, 1)
	_, err = f.store.Apply(t.Context(), plan)
	require.NoError(t, err)
	require.Equal(t, "before", contents(t, p))
}
func TestSymlinkAndStalePreviewRefused(t *testing.T) {
	f := setup(t)
	p := filepath.Join(f.root, "file")
	other := filepath.Join(f.root, "other")
	f.change(t, "a", p, state("before"), state("after"))
	f.write(t, p, "after")
	plan, err := f.store.Preview(t.Context(), "chat", "")
	require.NoError(t, err)
	require.NoError(t, f.q.SwitchTree(t.Context(), "chat", "a"))
	_, err = f.store.Apply(t.Context(), plan)
	require.ErrorContains(t, err, "chat moved")
	f.write(t, other, "after")
	require.NoError(t, os.Remove(p))
	require.NoError(t, os.Symlink(other, p))
	plan, err = f.store.Preview(context.Background(), "chat", "")
	require.NoError(t, err)
	require.Contains(t, plan.Entries[0].Conflict, "symbolic link")
}

func TestBudgetEvictsOldestChatFirst(t *testing.T) {
	f := setup(t)
	p := filepath.Join(f.root, "file")
	data := make([]byte, 2048)
	_, err := rand.Read(data)
	require.NoError(t, err)
	f.change(t, "old", p, nil, &filechange.State{Mode: 0644, Size: 2048, RestoreData: base64.StdEncoding.EncodeToString(data)})
	_, err = f.q.CreateSession(t.Context(), db.CreateSessionParams{ID: "new", Title: "new"})
	require.NoError(t, err)
	_, err = f.q.CreateMessage(t.Context(), db.CreateMessageParams{ID: "new", SessionID: "new", Role: "tool", Parts: "[]"})
	require.NoError(t, err)
	_, err = rand.Read(data)
	require.NoError(t, err)
	require.NoError(t, f.store.Capture(t.Context(), "new", "new", "call", &filechange.Review{Root: f.root, Changes: []filechange.Change{{Path: p, After: &filechange.State{Mode: 0644, Size: 2048, RestoreData: base64.StdEncoding.EncodeToString(data)}}}}))
	f.store.Budget = 2300
	require.NoError(t, f.store.Maintain(t.Context()))
	var oldSaved, newSaved int
	require.NoError(t, f.conn.QueryRow(`SELECT json_extract(after_state,'$.saved') FROM file_history_changes WHERE session_id='chat'`).Scan(&oldSaved))
	require.NoError(t, f.conn.QueryRow(`SELECT json_extract(after_state,'$.saved') FROM file_history_changes WHERE session_id='new'`).Scan(&newSaved))
	require.Zero(t, oldSaved)
	require.Equal(t, 1, newSaved)
}

func TestNewMessageInvalidatesApprovedPreview(t *testing.T) {
	f := setup(t)
	p := filepath.Join(f.root, "file")
	f.change(t, "a", p, state("before"), state("after"))
	f.write(t, p, "after")
	plan, err := f.store.Preview(t.Context(), "chat", "")
	require.NoError(t, err)
	_, err = f.q.CreateMessage(t.Context(), db.CreateMessageParams{ID: "new-message", SessionID: "chat", Role: "user", Parts: "[]"})
	require.NoError(t, err)
	_, err = f.store.Apply(t.Context(), plan)
	require.ErrorContains(t, err, "chat moved")
	require.Equal(t, "after", contents(t, p))
}
