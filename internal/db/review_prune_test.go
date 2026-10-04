package db

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIdleChatsLoseSavedDiffsAfterThreeDays(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	conn, err := Connect(t.Context(), dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = Release(dir) })

	now := time.Now()
	old := now.AddDate(0, 0, -4).Unix()
	for _, session := range []struct{ id, parent string }{{"idle", ""}, {"idle-child", "idle"}, {"active", ""}, {"active-child", "active"}} {
		_, err := conn.ExecContext(t.Context(), `INSERT INTO sessions(id, parent_session_id, title, updated_at, created_at) VALUES (?, NULLIF(?, ''), 't', 0, 0)`, session.id, session.parent)
		require.NoError(t, err)
		_, err = conn.ExecContext(t.Context(), `INSERT INTO messages(id, session_id, role, parts, created_at, updated_at) VALUES (?, ?, 'tool', '[]', 0, 0)`, session.id+"-msg", session.id)
		require.NoError(t, err)
		_, err = conn.ExecContext(t.Context(), `INSERT INTO message_review_details(message_id, payload) VALUES (?, x'00')`, session.id+"-msg")
		require.NoError(t, err)
	}
	// Triggers stamp updated_at with the current time, so set it after them.
	_, err = conn.ExecContext(t.Context(), `DROP TRIGGER update_sessions_updated_at`)
	require.NoError(t, err)
	for id, updated := range map[string]int64{"idle": old, "idle-child": old, "active": old, "active-child": now.Unix()} {
		_, err = conn.ExecContext(t.Context(), `UPDATE sessions SET updated_at = ? WHERE id = ?`, updated, id)
		require.NoError(t, err)
	}

	removed, err := PruneIdleReviews(t.Context(), conn, now.AddDate(0, 0, -ReviewIdleDays))
	require.NoError(t, err)
	require.EqualValues(t, 2, removed, "the idle chat and its sub-agent lose their diffs")
	var kept []string
	rows, err := conn.QueryContext(t.Context(), `SELECT message_id FROM message_review_details ORDER BY message_id`)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		kept = append(kept, id)
	}
	require.Equal(t, []string{"active-child-msg", "active-msg"}, kept, "a chat used recently keeps every diff, including old ones")
	var messages int
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*) FROM messages`).Scan(&messages))
	require.Equal(t, 4, messages, "messages are never deleted")
}
