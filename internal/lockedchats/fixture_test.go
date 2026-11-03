package lockedchats

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
)

// canary is the sentence a locked chat's every copy holds; after a lock it
// must be nowhere outside the vault.
const canary = "The heron counts nine copper lanterns at dawn."

// fixture is a folder with one chat to lock: a sub-agent chat under it, a
// fork made from it, a branch link to the chat it came from, saved file
// versions (one shared with another chat), a transcript, its entries in
// Crush's shared files and fake Claude Code and Codex stores. The vault is
// a plain folder; no DBus.
type fixture struct {
	t        *testing.T
	ctx      context.Context
	dir      string
	dataDir  string
	home     string
	conn     *sql.DB
	sessions session.Service
	vault    *DirVault
	svc      *Service

	root, child, fork, parent string
	shared, own               string // saved-file digests
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := t.Context()
	dir := t.TempDir()
	f := &fixture{t: t, ctx: ctx, dir: dir, dataDir: filepath.Join(dir, ".crush"), home: filepath.Join(dir, "home")}

	oldHomes, oldRegistry, oldWait, oldHook := homes, registryPath, retryWait, crashHook
	t.Cleanup(func() { homes, registryPath, retryWait, crashHook = oldHomes, oldRegistry, oldWait, oldHook })
	homes = func() map[string]string {
		return map[string]string{
			"claude-code": filepath.Join(f.home, ".claude"),
			"codex-cli":   filepath.Join(f.home, ".codex"),
			"grok-cli":    filepath.Join(f.home, ".grok"),
			"opencode":    filepath.Join(f.home, "opencode"),
			"agy-cli":     filepath.Join(f.home, "agy"),
		}
	}
	registryPath = func() string { return filepath.Join(dir, "run", "unlocked-chats.json") }
	retryWait = 10 * time.Millisecond

	conn, err := db.Connect(ctx, f.dataDir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Release(f.dataDir) })
	f.conn = conn
	q := db.New(conn)
	f.sessions = session.NewService(q, conn)
	messages := message.NewService(q)
	f.vault = &DirVault{Dir: filepath.Join(dir, "vault")}
	f.svc = f.newService()

	say := func(id string, role message.MessageRole, text string) message.Message {
		m, err := messages.Create(ctx, id, message.CreateMessageParams{Role: role, Parts: []message.ContentPart{message.TextContent{Text: text}}})
		require.NoError(t, err)
		return m
	}
	parent, err := f.sessions.Create(ctx, "where it came from")
	require.NoError(t, err)
	f.parent = parent.ID
	parentMsg := say(f.parent, message.User, "an ordinary question")

	root, err := f.sessions.Create(ctx, "the secret chat")
	require.NoError(t, err)
	f.root = root.ID
	first := say(f.root, message.User, "Remember this: "+canary)
	say(f.root, message.Assistant, "I will remember: "+canary)
	child, err := f.sessions.CreateTaskSession(ctx, "call-helper-1", f.root, "helper")
	require.NoError(t, err)
	f.child = child.ID
	say(f.child, message.User, "helper task about "+canary)
	say(f.child, message.Assistant, "helper report")
	say(f.root, message.User, "and one more turn")

	fork, err := f.sessions.Create(ctx, "a fork")
	require.NoError(t, err)
	f.fork = fork.ID
	say(f.fork, message.User, "the fork's own text")

	exec := func(query string, args ...any) {
		_, err := conn.ExecContext(ctx, query, args...)
		require.NoError(t, err, query)
	}
	// The secret chat is a branch of parent; fork is a branch of it.
	exec(`INSERT INTO chat_branches (id, session_id, parent_session_id, source_message_id, highlight_text, highlight_start, highlight_end, created_at) VALUES ('b-in', ?, ?, ?, 'ordinary', 3, 11, 1)`, f.root, f.parent, parentMsg.ID)
	exec(`INSERT INTO chat_branches (id, session_id, parent_session_id, source_message_id, highlight_text, highlight_start, highlight_end, created_at) VALUES ('b-out', ?, ?, ?, 'Remember', 0, 8, 2)`, f.fork, f.root, first.ID)

	// Saved file versions: shared is also the parent chat's, own is not.
	f.shared, f.own = sha256Hex([]byte("shared version")), sha256Hex([]byte(canary))
	for _, d := range []string{f.shared, f.own} {
		path := filepath.Join(f.dataDir, "file-history", "objects", d[:2], d)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte("gz:"+d), 0o600))
		exec(`INSERT INTO file_history_objects (digest, size) VALUES (?, 10)`, d)
	}
	state := func(d string) string { return `{"digest":"` + d + `","saved":true}` }
	exec(`INSERT INTO file_history_changes (message_id, session_id, tool_id, path, capture_order, before_state, after_state) VALUES (?, ?, 't1', 'a.txt', 0, ?, ?)`, first.ID, f.root, state(f.shared), state(f.own))
	exec(`INSERT INTO file_history_changes (message_id, session_id, tool_id, path, capture_order, before_state, after_state) VALUES (?, ?, 't0', 'a.txt', 0, ?, ?)`, parentMsg.ID, f.parent, state(f.shared), state(f.shared))
	exec(`INSERT INTO file_history_points (message_id, session_id, git_root, git_head) VALUES (?, ?, '/r', 'abc')`, first.ID, f.root)
	exec(`INSERT INTO files (id, session_id, path, content, version, created_at, updated_at) VALUES ('file-1', ?, 'notes.txt', ?, 0, 1, 1)`, f.root, canary)
	exec(`INSERT INTO read_files (session_id, path, read_at) VALUES (?, 'notes.txt', 1)`, f.child)
	exec(`INSERT INTO cli_message_markers (message_id, session_id, cli, native_session, marker, marker_kind, created_at) VALUES (?, ?, 'codex-cli', 'nat-codex', 'turn-1', 'turn', 1)`, first.ID, f.root)
	exec(`UPDATE sessions SET todos = ? WHERE id = ?`, `[{"content":"`+canary+`","status":"pending","active_form":"x"}]`, f.root)
	// Old list times the round trip must keep.
	exec(`UPDATE sessions SET updated_at = 1000 WHERE id = ?`, f.root)
	exec(`UPDATE sessions SET updated_at = 2000 WHERE id = ?`, f.child)

	// Files outside crush.db.
	f.write(filepath.Join(f.dataDir, "transcripts", safeName(f.root)+".md"), "# transcript\n"+canary+"\n")
	f.write(filepath.Join(f.dataDir, "cli-sessions.json"), `{"`+f.root+`/claude-code":{"native":"nat-claude","through":"x"},"`+f.child+`/codex-cli":{"native":"nat-codex","through":"y"},"`+f.parent+`/claude-code":{"native":"other-claude","through":"z"}}`)
	f.write(filepath.Join(f.dataDir, "task-history.json"), `[{"id":"1","session_id":"`+f.root+`","child_id":"`+f.child+`","name":"`+canary+`"},{"id":"2","session_id":"`+f.parent+`","child_id":"x","name":"other"}]`)
	f.write(f.svc.PromptHistory, `["an old prompt",{"text":"Remember this: `+canary+`","chat":"`+f.root+`"}]`)
	f.write(filepath.Join(f.home, ".claude", "projects", "-proj", "nat-claude.jsonl"), `{"sessionId":"nat-claude","text":"`+canary+`"}`+"\n")
	f.write(filepath.Join(f.home, ".claude", "projects", "-proj", "other-claude.jsonl"), `{"sessionId":"other-claude"}`+"\n")
	f.write(filepath.Join(f.home, ".claude", "history.jsonl"), `{"display":"hi","sessionId":"other-claude"}`+"\n"+`{"display":"`+canary+`","sessionId":"nat-claude"}`+"\n")
	f.write(filepath.Join(f.home, ".codex", "sessions", "2026", "11", "03", "rollout-2026-11-03T00-00-00-nat-codex.jsonl"), `{"text":"`+canary+`"}`+"\n")
	f.write(filepath.Join(f.home, ".codex", "history.jsonl"), `{"session_id":"nat-codex","text":"`+canary+`"}`+"\n"+`{"session_id":"other","text":"hello"}`+"\n")
	codex, err := db.OpenForeign(filepath.Join(f.home, ".codex", "state_5.sqlite"))
	require.NoError(t, err)
	for _, stmt := range []string{
		`CREATE TABLE threads (id TEXT PRIMARY KEY, title TEXT NOT NULL, first_user_message TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE thread_dynamic_tools (thread_id TEXT NOT NULL, position INTEGER NOT NULL, name TEXT NOT NULL, PRIMARY KEY(thread_id, position))`,
		`INSERT INTO threads VALUES ('nat-codex', 'secret', '` + canary + `'), ('other', 'other', 'hello')`,
		`INSERT INTO thread_dynamic_tools VALUES ('nat-codex', 0, 'tool'), ('other', 0, 'tool')`,
	} {
		_, err := codex.ExecContext(ctx, stmt)
		require.NoError(t, err)
	}
	require.NoError(t, codex.Close())
	return f
}

// newService is a fresh service on the fixture, as a restarted Crush has.
func (f *fixture) newService() *Service {
	return &Service{
		DB: f.conn, DataDir: f.dataDir, Folder: f.dir, Sessions: f.sessions, Vault: f.vault,
		PromptHistory: filepath.Join(f.dir, "prompt-history.json"),
		CacheDir:      filepath.Join(f.dir, "cache"),
	}
}

func (f *fixture) write(path, text string) {
	f.t.Helper()
	require.NoError(f.t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(f.t, os.WriteFile(path, []byte(text), 0o600))
}

func (f *fixture) state() string {
	f.t.Helper()
	state, _, _, err := f.svc.state(f.ctx, f.root)
	require.NoError(f.t, err)
	return state
}

// lock locks the chat and waits for it.
func (f *fixture) lock() {
	f.t.Helper()
	require.NoError(f.t, f.svc.Lock(f.ctx, f.root))
	f.svc.Wait()
}

// unlock unlocks the chat and waits for it.
func (f *fixture) unlock() {
	f.t.Helper()
	require.NoError(f.t, f.svc.Unlock(f.ctx, f.root))
	f.svc.Wait()
}

// chatRows counts the chat's (and its sub-agent's) rows of every table in
// crush.db.
func (f *fixture) chatRows() map[string]int64 {
	f.t.Helper()
	conn, err := f.conn.Conn(f.ctx)
	require.NoError(f.t, err)
	defer conn.Close()
	require.NoError(f.t, setLockSet(f.ctx, conn, []string{f.root, f.child}))
	defer dropLockSet(conn)
	counts, err := countRows(f.ctx, conn, "main")
	require.NoError(f.t, err)
	return counts
}

// outsideHas reports whether crush.db still holds the chat's messages.
func (f *fixture) outsideHas() bool {
	var n int
	require.NoError(f.t, f.conn.QueryRowContext(f.ctx, `SELECT count(*) FROM messages WHERE session_id IN (?, ?)`, f.root, f.child).Scan(&n))
	return n > 0
}

// vaultHas reports whether the vault holds a complete copy with the
// chat's messages.
func (f *fixture) vaultHas() bool {
	dir := filepath.Join(f.vault.Dir, "chats", safeName(f.root))
	if !fileExists(filepath.Join(dir, "complete")) {
		return false
	}
	conn, err := db.OpenForeign(filepath.Join(dir, "crush.db"))
	require.NoError(f.t, err)
	defer conn.Close()
	var n int
	if err := conn.QueryRowContext(f.ctx, `SELECT count(*) FROM messages WHERE session_id IN (?, ?)`, f.root, f.child).Scan(&n); err != nil {
		return false
	}
	return n == 5
}

// treeOf maps each of the chat's messages to its parent in the branch tree.
func (f *fixture) treeOf() map[string]string {
	rows, err := f.conn.QueryContext(f.ctx, `SELECT message_id, coalesce(parent_id, '') FROM tree_nodes WHERE session_id IN (?, ?)`, f.root, f.child)
	require.NoError(f.t, err)
	defer rows.Close()
	tree := map[string]string{}
	for rows.Next() {
		var id, parent string
		require.NoError(f.t, rows.Scan(&id, &parent))
		tree[id] = parent
	}
	return tree
}

func (f *fixture) updatedAt(id string) int64 {
	var at int64
	require.NoError(f.t, f.conn.QueryRowContext(f.ctx, `SELECT updated_at FROM sessions WHERE id = ?`, id).Scan(&at))
	return at
}

// jsonKeys lists a JSON object file's keys.
func jsonKeys(t *testing.T, path string) []string {
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &m))
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func (f *fixture) title() string {
	f.t.Helper()
	var title string
	require.NoError(f.t, f.conn.QueryRow(`SELECT title FROM sessions WHERE id = ?`, f.root).Scan(&title))
	return title
}
