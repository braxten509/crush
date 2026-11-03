package lockedchats

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/session"
)

// A second connection commits after the snapshot, before deletion. The lock
// must abort with every live row and outside file intact.
func TestLockRejectsConcurrentMessage(t *testing.T) {
	f := newFixture(t)
	other, err := db.OpenForeign(filepath.Join(f.dataDir, "crush.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = other.Close() })
	var reported string
	f.svc.Notify = func(_ string, text string) { reported = text }
	crashHook = func(step string) error {
		if step == "lock:copied" {
			_, err := other.ExecContext(f.ctx, `INSERT INTO messages (id, session_id, role, parts, created_at, updated_at) VALUES ('late-message', ?, 'user', '[]', 3, 3)`, f.root)
			return err
		}
		return nil
	}
	f.lock()
	require.Contains(t, reported, "changed during locking")
	require.Empty(t, f.state())
	var count int
	require.NoError(t, other.QueryRowContext(f.ctx, `SELECT count(*) FROM messages WHERE session_id IN (?, ?)`, f.root, f.child).Scan(&count))
	require.Equal(t, 6, count)
	require.FileExists(t, filepath.Join(f.dataDir, "transcripts", safeName(f.root)+".md"))
	crashHook = func(string) error { return nil }
	f.lock()
	require.Equal(t, session.LockStateLocked, f.state())
	// Even a caller that passed its early check cannot insert after locking.
	_, err = db.New(other).CreateMessage(f.ctx, db.CreateMessageParams{ID: "too-late", SessionID: f.root, Role: "user", Parts: "[]"})
	require.ErrorIs(t, err, sql.ErrNoRows)
}

// Locking takes every row of the chat and its sub-agent out of crush.db
// into the vault; unlocking puts the same rows back: same counts, same
// branch tree, same list times, links to the fork and the chat it came
// from, and no broken foreign keys.
func TestLockUnlockRoundTripKeepsEveryRow(t *testing.T) {
	f := newFixture(t)
	before := f.chatRows()
	tree := f.treeOf()
	require.NotEmpty(t, tree)
	require.EqualValues(t, 2, before["sessions"])
	require.EqualValues(t, 5, before["messages"])

	f.lock()
	require.Equal(t, session.LockStateLocked, f.state())
	locked := f.chatRows()
	for table, n := range locked {
		switch table {
		case "sessions":
			require.EqualValues(t, 1, n, "only the chat's own row stays, for the list")
		case "file_history_objects":
			require.EqualValues(t, 0, n, "no saved version of the chat stays listed")
		default:
			require.Zero(t, n, "rows of %s left in crush.db", table)
		}
	}
	var links int
	require.NoError(t, f.conn.QueryRow(`SELECT count(*) FROM chat_branches`).Scan(&links))
	require.Zero(t, links, "branch links to and from the locked chat leave with it")
	require.EqualValues(t, 1000, f.updatedAt(f.root), "locking must not move the chat in the list")
	require.True(t, f.vaultHas())
	// Its name can quote the secret, so the list shows a stand-in (owner,
	// 2026-11-03) and the name cannot change while locked.
	require.Equal(t, session.LockedTitle, f.title())
	require.ErrorIs(t, f.sessions.Rename(f.ctx, f.root, "new name"), session.ErrChatLocked)
	// The shared saved version stays for the other chat; the chat's own goes.
	require.FileExists(t, filepath.Join(f.dataDir, "file-history", "objects", f.shared[:2], f.shared))
	require.NoFileExists(t, filepath.Join(f.dataDir, "file-history", "objects", f.own[:2], f.own))

	f.unlock()
	require.Equal(t, session.LockStateUnlocked, f.state())
	require.Equal(t, "the secret chat", f.title())
	require.Equal(t, before, f.chatRows())
	require.Equal(t, tree, f.treeOf())
	require.EqualValues(t, 1000, f.updatedAt(f.root))
	require.EqualValues(t, 2000, f.updatedAt(f.child))
	require.NoError(t, f.conn.QueryRow(`SELECT count(*) FROM chat_branches`).Scan(&links))
	require.Equal(t, 2, links, "both branch links come back")
	rows, err := f.conn.Query(`PRAGMA foreign_key_check`)
	require.NoError(t, err)
	require.False(t, rows.Next(), "foreign key check must be clean")
	require.NoError(t, rows.Close())
	var todos string
	require.NoError(t, f.conn.QueryRow(`SELECT todos FROM sessions WHERE id = ?`, f.root).Scan(&todos))
	require.Contains(t, todos, canary)
	require.FileExists(t, filepath.Join(f.dataDir, "file-history", "objects", f.own[:2], f.own))
	require.Contains(t, jsonKeys(t, filepath.Join(f.dataDir, "cli-sessions.json")), f.root+"/claude-code")
	require.FileExists(t, filepath.Join(f.home, ".claude", "projects", "-proj", "nat-claude.jsonl"))

	// Locked again (the lock button on an unlocked chat), then back once more.
	f.lock()
	require.Equal(t, session.LockStateLocked, f.state())
	f.unlock()
	require.Equal(t, before, f.chatRows())
	require.Equal(t, tree, f.treeOf())
}
