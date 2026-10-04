package db

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
)

func TestConnectUpgradesForkDatabaseWithOlderUpstreamMigrations(t *testing.T) {
	dataDir := t.TempDir()
	t.Cleanup(func() { require.NoError(t, Release(dataDir)) })
	conn, err := openDB(filepath.Join(dataDir, "crush.db"))
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })

	// Build a pre-merge fork database: it has the newer review migration,
	// but neither of the upstream MCP toggle migrations.
	entries, err := fs.ReadDir(FS, "migrations")
	require.NoError(t, err)
	legacy := fstest.MapFS{}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), "_add_mcp_disabled_servers.sql") || strings.HasSuffix(entry.Name(), "_add_mcp_enabled_servers.sql") {
			continue
		}
		data, err := FS.ReadFile("migrations/" + entry.Name())
		require.NoError(t, err)
		legacy[entry.Name()] = &fstest.MapFile{Data: data}
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, conn, legacy)
	require.NoError(t, err)
	_, err = provider.Up(t.Context())
	require.NoError(t, err)
	_, err = New(conn).CreateSession(t.Context(), CreateSessionParams{ID: "saved-chat", Title: "Keep my chat"})
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), "INSERT INTO message_review_migrations(session_id) VALUES (?)", "saved-chat")
	require.NoError(t, err)
	require.NoError(t, conn.Close())

	upgraded, err := Connect(t.Context(), dataDir)
	require.NoError(t, err)
	queries := New(upgraded)
	chat, err := queries.GetSessionByID(t.Context(), "saved-chat")
	require.NoError(t, err)
	require.Equal(t, "Keep my chat", chat.Title)
	var retained int
	require.NoError(t, upgraded.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM message_review_migrations WHERE session_id = ?", "saved-chat").Scan(&retained))
	require.Equal(t, 1, retained)
	require.NoError(t, queries.InsertMCPDisabledServer(t.Context(), "disabled"))
	require.NoError(t, queries.InsertMCPEnabledServer(t.Context(), "enabled"))
	require.NoError(t, Release(dataDir))

	// The next startup must preserve both the chat and its new overrides.
	reopened, err := Connect(t.Context(), dataDir)
	require.NoError(t, err)
	disabled, err := New(reopened).ListMCPDisabledServers(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"disabled"}, disabled)
	enabled, err := New(reopened).ListMCPEnabledServers(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"enabled"}, enabled)
}
