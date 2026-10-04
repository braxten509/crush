package db

import (
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

func TestTreeMigrationKeepsLinearOrderAndSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	ctx := t.Context()
	conn, err := openDB(filepath.Join(dir, "crush.db"))
	require.NoError(t, err)
	require.NoError(t, initGoose())
	require.NoError(t, goose.UpToContext(ctx, conn, "migrations", 20261003000000, goose.WithAllowMissing()))
	_, err = conn.Exec(`INSERT INTO sessions(id,title,created_at,updated_at) VALUES('s','old chat',1,1);
 INSERT INTO messages(id,session_id,role,created_at,updated_at) VALUES('z','s','user',1,1),('a','s','assistant',1,1),('b','s','user',2,2)`)
	require.NoError(t, err)
	require.NoError(t, conn.Close())
	conn, err = Connect(ctx, dir)
	require.NoError(t, err)
	q := New(conn)
	ids, err := q.TreePath(ctx, "s")
	require.NoError(t, err)
	require.Equal(t, []string{"z", "a", "b"}, ids)
	require.NoError(t, q.SwitchTree(ctx, "s", "a"))
	require.NoError(t, q.LabelTree(ctx, "s", "b", "old end"))
	require.NoError(t, Release(dir))
	conn, err = Connect(ctx, dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = Release(dir) })
	q = New(conn)
	ids, err = q.TreePath(ctx, "s")
	require.NoError(t, err)
	require.Equal(t, []string{"z", "a"}, ids)
	nodes, err := q.TreeNodes(ctx, "s")
	require.NoError(t, err)
	require.Len(t, nodes, 3)
	require.Equal(t, "old end", nodes[2].Label)
}
