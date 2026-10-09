package sessioncatalog

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/projects"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/stretchr/testify/require"
)

func catalogFixture(t *testing.T) (Catalog, *sql.DB) {
	t.Helper()
	directory, data := t.TempDir(), t.TempDir()
	conn, err := db.Connect(t.Context(), data)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Release(data)) })
	require.NoError(t, projects.Register(directory, data))
	return Catalog{Directory: directory, DataDirectory: data}, conn
}
func catalogChat(t *testing.T, conn *sql.DB, id, parent, role, parts string, stamp int64) {
	t.Helper()
	var parentID any
	if parent != "" {
		parentID = parent
	}
	_, err := conn.ExecContext(t.Context(), "INSERT INTO sessions(id,parent_session_id,title,created_at,updated_at) VALUES(?,?,?,?,?)", id, parentID, id, stamp, stamp)
	require.NoError(t, err)
	if role != "" {
		_, err = conn.ExecContext(t.Context(), "INSERT INTO messages(id,session_id,role,parts,created_at,updated_at,finished_at) VALUES(?,?,?,?,?,?,?)", id+"-message", id, role, parts, stamp, stamp, stamp)
		require.NoError(t, err)
	}
	_, err = conn.ExecContext(t.Context(), "UPDATE sessions SET updated_at=? WHERE id=?", stamp, id)
	require.NoError(t, err)
}
func catalogExists(t *testing.T, conn *sql.DB, id string) bool {
	t.Helper()
	var exists bool
	require.NoError(t, conn.QueryRowContext(t.Context(), "SELECT EXISTS(SELECT 1 FROM sessions WHERE id=?)", id).Scan(&exists))
	return exists
}
func TestCatalogListsAllFoldersAndDiscardsOnlyAbandonedEmptyChats(t *testing.T) {
	t.Setenv("CRUSH_GLOBAL_DATA", filepath.Join(t.TempDir(), "data"))
	first, a := catalogFixture(t)
	second, b := catalogFixture(t)
	old := time.Now().Add(-time.Hour).Unix()
	catalogChat(t, a, "text", "", "user", `[{"type":"text","text":"hello"}]`, old)
	catalogChat(t, b, "attachment", "", "user", `[{"type":"binary","mime_type":"image/png"}]`, old+1)
	catalogChat(t, a, "empty", "", "", "", old)
	catalogChat(t, b, "assistant-only", "", "assistant", `[{"type":"text","text":"draft"}]`, old)
	catalogChat(t, a, "selected-empty", "", "", "", old)
	catalogChat(t, b, "recent-empty", "", "", "", time.Now().Unix())
	catalogChat(t, b, "child", "attachment", "user", `[]`, old+2)
	// An alias of a data directory must not repeat its chats.
	require.NoError(t, projects.Register(t.TempDir(), first.DataDirectory))
	require.NoError(t, projects.Register(t.TempDir(), filepath.Join(t.TempDir(), "missing")))
	entries, err := first.List(t.Context(), "selected-empty")
	require.NoError(t, err)
	require.Len(t, entries, 2)
	byID := map[string]session.Session{}
	for _, entry := range entries {
		byID[entry.ID] = entry
	}
	require.Equal(t, second.Directory, byID["attachment"].Directory)
	require.Equal(t, second.DataDirectory, byID["attachment"].DataDirectory)
	require.Equal(t, first.Directory, byID["text"].Directory)
	require.GreaterOrEqual(t, entries[0].UpdatedAt, entries[1].UpdatedAt)
	require.False(t, catalogExists(t, a, "empty"))
	require.False(t, catalogExists(t, b, "assistant-only"))
	require.True(t, catalogExists(t, a, "selected-empty"))
	require.True(t, catalogExists(t, b, "recent-empty"))
	require.True(t, catalogExists(t, b, "child"))
}
func TestCatalogChangesStayInTheOriginalDatabase(t *testing.T) {
	t.Setenv("CRUSH_GLOBAL_DATA", filepath.Join(t.TempDir(), "data"))
	first, a := catalogFixture(t)
	second, b := catalogFixture(t)
	stamp := time.Now().Unix()
	catalogChat(t, a, "same-id", "", "user", `[]`, stamp)
	catalogChat(t, b, "same-id", "", "user", `[]`, stamp)
	target := session.Session{ID: "same-id", Title: "renamed", Directory: second.Directory, DataDirectory: second.DataDirectory}
	require.NotEqual(t, (session.Session{ID: "same-id", DataDirectory: first.DataDirectory}).CatalogID(), target.CatalogID())
	require.NoError(t, first.Change(t.Context(), target, false))
	var title string
	require.NoError(t, a.QueryRow("SELECT title FROM sessions WHERE id='same-id'").Scan(&title))
	require.Equal(t, "same-id", title)
	require.NoError(t, b.QueryRow("SELECT title FROM sessions WHERE id='same-id'").Scan(&title))
	require.Equal(t, "renamed", title)
	require.NoError(t, first.Change(t.Context(), target, true))
	require.True(t, catalogExists(t, a, "same-id"))
	require.False(t, catalogExists(t, b, "same-id"))
	target.DataDirectory = t.TempDir()
	require.ErrorContains(t, first.Change(context.Background(), target, false), "no longer registered")
}

func TestDiscardEmptyPreservesSentMessages(t *testing.T) {
	t.Setenv("CRUSH_GLOBAL_DATA", filepath.Join(t.TempDir(), "data"))
	catalog, conn := catalogFixture(t)
	now := time.Now().Unix()
	catalogChat(t, conn, "draft", "", "", "", now)
	catalogChat(t, conn, "attachment", "", "user", `[{"type":"binary"}]`, now)
	require.NoError(t, catalog.DiscardEmpty(t.Context(), "draft"))
	require.False(t, catalogExists(t, conn, "draft"))
	require.NoError(t, catalog.DiscardEmpty(t.Context(), "attachment"))
	require.True(t, catalogExists(t, conn, "attachment"))
}
