package lockedchats

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/stretchr/testify/require"
)

func TestLockKeepsSharedAttachment(t *testing.T) {
	f := newFixture(t)
	data := []byte("a shared attachment")
	path := filepath.Join(f.svc.CacheDir, "attachments", sha256Hex(data)+".png")
	f.write(path, string(data))
	messages := message.NewService(db.New(f.conn))
	for _, id := range []string{f.root, f.parent} {
		_, err := messages.Create(f.ctx, id, message.CreateMessageParams{Role: message.User, Parts: []message.ContentPart{message.BinaryContent{Data: data, MIMEType: "image/png"}}})
		require.NoError(t, err)
	}
	f.lock()
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, data, got)
	require.NotEmpty(t, filesWith(t, f.vault.Dir, string(data)), "a copy must also go into the vault")
}

// After a lock the canary sentence is in no file outside the vault:
// crush.db and its log, transcripts, Crush's shared files, and the agent
// CLIs' stores. The vault still holds it.
func TestLockLeavesNoCopyOutsideTheVault(t *testing.T) {
	f := newFixture(t)
	require.NotEmpty(t, filesWith(t, f.dir, canary), "the fixture must hold the canary before the lock")

	f.lock()

	vault := f.vault.Dir + string(filepath.Separator)
	var leaks []string
	for _, path := range filesWith(t, f.dir, canary) {
		if len(path) < len(vault) || path[:len(vault)] != vault {
			leaks = append(leaks, path)
		}
	}
	require.Empty(t, leaks, "the canary is still outside the vault")
	require.NotEmpty(t, filesWith(t, f.vault.Dir, canary), "the vault must hold the chat")

	// Other chats' entries in the shared files stay.
	require.Equal(t, []string{f.parent + "/claude-code"}, jsonKeys(t, filepath.Join(f.dataDir, "cli-sessions.json")))
	history, err := os.ReadFile(filepath.Join(f.home, ".claude", "history.jsonl"))
	require.NoError(t, err)
	require.Contains(t, string(history), "other-claude")
	require.FileExists(t, filepath.Join(f.home, ".claude", "projects", "-proj", "other-claude.jsonl"))
	prompts, err := os.ReadFile(f.svc.PromptHistory)
	require.NoError(t, err)
	require.Contains(t, string(prompts), "an old prompt")
	info, err := os.Stat(filepath.Join(f.dataDir, "crush.db-wal"))
	if err == nil {
		require.Zero(t, info.Size(), "crush.db's log must be emptied")
	}
}

// filesWith lists the files under dir holding text.
func filesWith(t *testing.T, dir, text string) []string {
	t.Helper()
	var hits []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(data, []byte(text)) {
			hits = append(hits, path)
		}
		return nil
	})
	require.NoError(t, err)
	return hits
}

// A CLI deletes its own rows without wiping them, so its database keeps
// old copies of the chat in freed space. Locking must clear that too
// (leak test, 2026-11-03).
func TestLockClearsSpaceTheCLIFreedEarlier(t *testing.T) {
	f := newFixture(t)
	codex, err := db.OpenForeign(filepath.Join(f.home, ".codex", "state_5.sqlite"))
	require.NoError(t, err)
	draft := strings.Repeat(canary+" ", 400)
	for _, stmt := range []string{
		`PRAGMA secure_delete = OFF`,
		`INSERT INTO threads VALUES ('nat-codex-draft', 'draft', '` + draft + `')`,
		`DELETE FROM threads WHERE id = 'nat-codex-draft'`,
		`PRAGMA wal_checkpoint(TRUNCATE)`,
	} {
		_, err := codex.ExecContext(f.ctx, stmt)
		require.NoError(t, err, stmt)
	}
	require.NoError(t, codex.Close())

	f.lock()

	require.Empty(t, filesWith(t, filepath.Join(f.home, ".codex"), canary))
}

// After a restart Crush's startup work must not write rows for a locked
// chat: no model picked for it, no review-migration marker.
func TestStartupLeavesLockedChatAlone(t *testing.T) {
	f := newFixture(t)
	f.lock()
	locked := f.chatRows()

	_, err := f.sessions.AdoptModels(f.ctx, func(context.Context, session.Session) session.ModelChoice {
		return session.ModelChoice(`{"model":"default"}`)
	})
	require.NoError(t, err)
	_, _ = message.NewService(db.New(f.conn)).List(f.ctx, f.root)

	require.Equal(t, locked, f.chatRows())
}
