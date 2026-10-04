package message

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/charmbracelet/crush/internal/filehistory"
	"github.com/stretchr/testify/require"
)

func TestFileHistoryPrecedesReviewBoundingAndOutlivesPruning(t *testing.T) {
	data := t.TempDir()
	conn, err := db.Connect(t.Context(), data)
	require.NoError(t, err)
	t.Cleanup(func() { db.Release(data) })
	q := db.New(conn)
	_, err = q.CreateSession(t.Context(), db.CreateSessionParams{ID: "chat", Title: "restore"})
	require.NoError(t, err)
	root, err := os.MkdirTemp(".", "restore-test-")
	require.NoError(t, err)
	root, err = filepath.Abs(root)
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(root) })
	versions := filehistory.New(conn, data)
	svc := NewService(q, WithFileHistory(versions))
	path := filepath.Join(root, "large.txt")
	before := strings.Repeat("a", 3<<20)
	after := "after\n"
	require.NoError(t, os.WriteFile(path, []byte(after), 0644))
	result, err := svc.Create(t.Context(), "chat", CreateMessageParams{Role: Tool, Parts: []ContentPart{ToolResult{ToolCallID: "call", Name: "edit", Review: &filechange.Review{Root: root, Changes: []filechange.Change{{Path: path, Before: &filechange.State{Content: before, Size: int64(len(before)), Mode: 0644}, After: &filechange.State{Content: after, Size: int64(len(after)), Mode: 0644}}}}}}})
	require.NoError(t, err)
	review, err := svc.LoadReview(t.Context(), result.ID)
	require.NoError(t, err)
	require.Empty(t, review.ToolResults()[0].Review.Changes[0].Before.Content)
	for i := 0; i < 7; i++ {
		_, err = svc.Create(t.Context(), "chat", CreateMessageParams{Role: User, Parts: []ContentPart{TextContent{Text: "next"}}})
		require.NoError(t, err)
	}
	_, err = svc.LoadReview(t.Context(), result.ID)
	require.ErrorIs(t, err, ErrReviewExpired)
	plan, err := versions.Preview(t.Context(), "chat", "")
	require.NoError(t, err)
	require.Len(t, plan.Entries, 1)
	_, err = versions.Apply(t.Context(), plan)
	require.NoError(t, err)
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, string(content))
}

func TestHistoryFailureDoesNotLoseCreatedOrUpdatedToolResult(t *testing.T) {
	data := t.TempDir()
	conn, err := db.Connect(t.Context(), data)
	require.NoError(t, err)
	t.Cleanup(func() { db.Release(data) })
	q := db.New(conn)
	_, err = q.CreateSession(t.Context(), db.CreateSessionParams{ID: "chat", Title: "test"})
	require.NoError(t, err)
	root, err := os.MkdirTemp(".", "restore-failure-")
	require.NoError(t, err)
	root, err = filepath.Abs(root)
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(root) })
	require.NoError(t, os.WriteFile(filepath.Join(data, "file-history"), []byte("blocked"), 0600))
	versions := filehistory.New(conn, data, root)
	svc := NewService(q, WithFileHistory(versions))
	review := &filechange.Review{Root: root, Changes: []filechange.Change{{Path: filepath.Join(root, "file"), Before: &filechange.State{Content: "old", Mode: 0644, Size: 3}, After: &filechange.State{Content: "new", Mode: 0644, Size: 3}}}}
	created, err := svc.Create(t.Context(), "chat", CreateMessageParams{Role: Tool, Parts: []ContentPart{ToolResult{ToolCallID: "call", Content: "tool succeeded", Review: review}}})
	require.NoError(t, err)
	loaded, err := svc.Get(t.Context(), created.ID)
	require.NoError(t, err)
	require.Equal(t, "tool succeeded", loaded.ToolResults()[0].Content)
	loaded.Parts = []ContentPart{ToolResult{ToolCallID: "next", Content: "updated tool", Review: review}}
	_, err = svc.(*service).write(t.Context(), loaded)
	require.NoError(t, err)
	loaded, err = svc.Get(t.Context(), created.ID)
	require.NoError(t, err)
	require.Equal(t, "updated tool", loaded.ToolResults()[0].Content)
	plan, err := versions.Preview(t.Context(), "chat", "")
	require.NoError(t, err)
	require.Contains(t, plan.Entries[0].Unavailable, "File history unavailable")
	// Even a broken optional point table must not stop ordinary messages.
	_, err = conn.Exec(`ALTER TABLE file_history_points RENAME TO broken_points`)
	require.NoError(t, err)
	_, err = svc.Create(t.Context(), "chat", CreateMessageParams{Role: User, Parts: []ContentPart{TextContent{Text: "still works"}}})
	require.NoError(t, err)
}
