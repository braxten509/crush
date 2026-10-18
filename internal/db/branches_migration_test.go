package db

import (
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
)

// The branch tables go up, down and up again. The migration runs on top of
// the initial schema it refers to: later migrations turn goose's version
// table into a view, so the whole chain cannot be walked down.
func TestChatBranchesMigrationUpDown(t *testing.T) {
	ctx := t.Context()
	conn, err := openDB(filepath.Join(t.TempDir(), "crush.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	read := func(name string) *fstest.MapFile {
		data, err := FS.ReadFile("migrations/" + name)
		require.NoError(t, err)
		return &fstest.MapFile{Data: data}
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, conn, fstest.MapFS{
		"20250424200609_initial.sql":       read("20250424200609_initial.sql"),
		"20261018000000_chat_branches.sql": read("20261018000000_chat_branches.sql"),
	})
	require.NoError(t, err)

	objects := func() []string {
		rows, err := conn.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE name LIKE 'chat_branches%' OR name LIKE 'cli_message_markers%' ORDER BY name`)
		require.NoError(t, err)
		defer rows.Close()
		var names []string
		for rows.Next() {
			var name string
			require.NoError(t, rows.Scan(&name))
			names = append(names, name)
		}
		require.NoError(t, rows.Err())
		return names
	}
	all := []string{
		"chat_branches", "chat_branches_message", "chat_branches_origin", "chat_branches_parent",
		"cli_message_markers", "cli_message_markers_session",
	}

	_, err = provider.Up(ctx)
	require.NoError(t, err)
	require.Equal(t, all, objects())

	_, err = provider.Down(ctx)
	require.NoError(t, err)
	require.Empty(t, objects())
	version, err := provider.GetDBVersion(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(20250424200609), version)

	_, err = provider.Up(ctx)
	require.NoError(t, err)
	require.Equal(t, all, objects())

	// The tables work after the round trip: one origin per branch.
	_, err = conn.ExecContext(ctx, `INSERT INTO sessions(id,title,created_at,updated_at) VALUES('p','parent',1,1),('b','branch',1,1);
 INSERT INTO messages(id,session_id,role,created_at,updated_at) VALUES('m','p','user',1,1)`)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `INSERT INTO chat_branches(id,session_id,parent_session_id,source_message_id,highlight_text,highlight_start,highlight_end,created_at) VALUES('l1','b','p','m','w',0,1,1)`)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `INSERT INTO chat_branches(id,session_id,parent_session_id,source_message_id,highlight_text,highlight_start,highlight_end,created_at) VALUES('l2','b','p','m','w',2,3,1)`)
	require.Error(t, err, "a second origin link for one branch")
	_, err = conn.ExecContext(ctx, `INSERT INTO chat_branches(id,session_id,parent_session_id,source_message_id,highlight_text,highlight_start,highlight_end,copied_from,created_at) VALUES('l3','b','p','m','w',2,3,'l1',1)`)
	require.NoError(t, err, "copied links are not origins")
	_, err = conn.ExecContext(ctx, `INSERT INTO cli_message_markers(message_id,session_id,cli,native_session,marker,marker_kind,created_at) VALUES('m','p','claude-code','n','u','message',1)`)
	require.NoError(t, err)
}
