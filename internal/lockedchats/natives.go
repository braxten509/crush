package lockedchats

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/charmbracelet/crush/internal/db"
)

// Where each agent CLI keeps a chat (PLAN §12, probe turns 2026-11-03). A
// locked chat's copies move into the vault too (owner, 2026-11-03). Shared
// files and databases lose only that chat's entries; a store whose format
// is not the one known here makes the lock refuse (fail closed).

// Native is one agent CLI conversation linked to a Crush chat.
type Native struct {
	Kind string `json:"kind"` // config.Type* of the CLI
	ID   string `json:"id"`   // the CLI's own session, thread or conversation ID
}

// storeItem is one place a native conversation lives.
type storeItem struct {
	// File or Dir: a file or folder that belongs to the conversation alone.
	File, Dir string
	// Entry: entries in a file or database other conversations share.
	Entry *entryRef
}

// entryRef names a conversation's entries in a shared file or database,
// so they can be copied out, removed again after a crash, and put back.
type entryRef struct {
	// Type is "lines" (a JSON-lines file: every line holding ID), "sqlite"
	// (rows whose Columns hold ID, or whose id is ID in Primary tables),
	// "proto" (top-level entries keyed by ID), "crush-links",
	// "crush-tasks" or "crush-prompts" (Crush's own shared files).
	Type    string   `json:"type"`
	Path    string   `json:"path"`
	ID      string   `json:"id"`
	IDs     []string `json:"ids,omitempty"`
	Columns []string `json:"columns,omitempty"`
	Primary []string `json:"primary,omitempty"`
	// After runs once the rows are gone ("fts-optimize:<table>").
	After []string `json:"after,omitempty"`
}

// cliHome is a CLI's own folder: env when set, else the folder under home.
func cliHome(env string, rel ...string) string {
	if dir := os.Getenv(env); env != "" && dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(append([]string{home}, rel...)...)
}

// homes lets tests point every CLI at a fake home.
var homes = func() map[string]string {
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		home, _ := os.UserHomeDir()
		data = filepath.Join(home, ".local", "share")
	}
	return map[string]string{
		"claude-code": cliHome("CLAUDE_CONFIG_DIR", ".claude"),
		"codex-cli":   cliHome("CODEX_HOME", ".codex"),
		"grok-cli":    cliHome("", ".grok"),
		"opencode":    filepath.Join(data, "opencode"),
		"agy-cli":     cliHome("", ".gemini", "antigravity-cli"),
	}
}

// existing keeps the paths that exist.
func existing(paths ...string) []string {
	var out []string
	for _, p := range paths {
		if _, err := os.Lstat(p); err == nil {
			out = append(out, p)
		}
	}
	return out
}

func glob(pattern string) []string {
	matches, _ := filepath.Glob(pattern)
	return matches
}

// storeItems lists where a native conversation lives. An unknown kind or
// a store in an unknown format is an error, so the lock refuses.
func storeItems(n Native) ([]storeItem, error) {
	if n.ID == "" || strings.ContainsAny(n.ID, "/\\*?[") || n.ID == "." || n.ID == ".." {
		return nil, fmt.Errorf("the %s conversation ID %q is not one Crush can move", n.Kind, n.ID)
	}
	h := homes()
	var items []storeItem
	addFiles := func(paths ...string) {
		for _, p := range existing(paths...) {
			if info, err := os.Lstat(p); err == nil && info.IsDir() {
				items = append(items, storeItem{Dir: p})
			} else {
				items = append(items, storeItem{File: p})
			}
		}
	}
	addEntry := func(ref entryRef) {
		if _, err := os.Stat(ref.Path); err == nil {
			items = append(items, storeItem{Entry: &ref})
		}
	}
	switch {
	case n.Kind == "claude-code":
		home := h["claude-code"]
		addFiles(glob(filepath.Join(home, "projects", "*", n.ID+".jsonl"))...)
		addFiles(glob(filepath.Join(home, "projects", "*", n.ID))...)
		// Older sub-agent transcripts sit beside the chat's own file.
		for _, path := range glob(filepath.Join(home, "projects", "*", "agent-*.jsonl")) {
			if fileHas(path, `"sessionId":"`+n.ID+`"`) {
				addFiles(path)
			}
		}
		addFiles(filepath.Join(home, "file-history", n.ID), filepath.Join(home, "session-env", n.ID),
			filepath.Join(home, "tasks", n.ID), filepath.Join(home, "debug", n.ID+".txt"),
			filepath.Join(home, "image-cache", n.ID))
		addFiles(glob(filepath.Join(home, "todos", n.ID+"-*"))...)
		addEntry(entryRef{Type: "lines", Path: filepath.Join(home, "history.jsonl"), ID: n.ID})
	case n.Kind == "codex-cli":
		home := h["codex-cli"]
		for _, dir := range []string{"sessions", "archived_sessions"} {
			_ = filepath.WalkDir(filepath.Join(home, dir), func(path string, d fs.DirEntry, err error) error {
				if err == nil && !d.IsDir() && strings.HasSuffix(d.Name(), "-"+n.ID+".jsonl") {
					addFiles(path)
				}
				return nil
			})
		}
		threadCols := []string{"thread_id", "parent_thread_id", "child_thread_id"}
		for _, path := range glob(filepath.Join(home, "state_*.sqlite")) {
			if err := needTables(path, "threads"); err != nil {
				return nil, err
			}
			addEntry(entryRef{Type: "sqlite", Path: path, ID: n.ID, Columns: threadCols, Primary: []string{"threads"}})
		}
		for _, path := range glob(filepath.Join(home, "thread_history_*.sqlite")) {
			if err := needTables(path, "thread_items"); err != nil {
				return nil, err
			}
			addEntry(entryRef{Type: "sqlite", Path: path, ID: n.ID, Columns: threadCols})
		}
		for _, path := range glob(filepath.Join(home, "logs_*.sqlite")) {
			if err := needTables(path, "logs"); err != nil {
				return nil, err
			}
			addEntry(entryRef{Type: "sqlite", Path: path, ID: n.ID, Columns: threadCols})
		}
		addEntry(entryRef{Type: "lines", Path: filepath.Join(home, "history.jsonl"), ID: n.ID})
	case n.Kind == "grok-cli":
		home := h["grok-cli"]
		addFiles(glob(filepath.Join(home, "sessions", "*", n.ID))...)
		for _, path := range glob(filepath.Join(home, "sessions", "*", "prompt_history.jsonl")) {
			addEntry(entryRef{Type: "lines", Path: path, ID: n.ID})
		}
		search := filepath.Join(home, "sessions", "session_search.sqlite")
		if _, err := os.Stat(search); err == nil {
			if err := needTables(search, "session_docs"); err != nil {
				return nil, err
			}
			addEntry(entryRef{Type: "sqlite", Path: search, ID: n.ID, Columns: []string{"session_id"}, After: []string{"fts-optimize:session_docs_fts"}})
		}
	case strings.HasPrefix(n.Kind, "opencode"):
		home := h["opencode"]
		store := filepath.Join(home, "opencode.db")
		if _, err := os.Stat(store); err == nil {
			if err := needTables(store, "session", "message", "part"); err != nil {
				return nil, err
			}
			addEntry(entryRef{Type: "sqlite", Path: store, ID: n.ID, Columns: []string{"session_id", "aggregate_id"}, Primary: []string{"session", "session_v2"}})
		}
		addFiles(glob(filepath.Join(home, "storage", "*", n.ID+".json"))...)
		addFiles(glob(filepath.Join(home, "storage", "*", n.ID))...)
	case n.Kind == "agy-cli":
		home := h["agy-cli"]
		addFiles(glob(filepath.Join(home, "conversations", n.ID+".db*"))...)
		addFiles(filepath.Join(home, "brain", n.ID), filepath.Join(home, "annotations", n.ID+".pbtxt"))
		summaries := filepath.Join(home, "conversation_summaries.db")
		if _, err := os.Stat(summaries); err == nil {
			if err := needTables(summaries, "conversation_summaries"); err != nil {
				return nil, err
			}
			addEntry(entryRef{Type: "sqlite", Path: summaries, ID: n.ID, Columns: []string{"conversation_id"}})
		}
		addEntry(entryRef{Type: "proto", Path: filepath.Join(home, "jetbox_summaries_proto.pb"), ID: n.ID})
		addEntry(entryRef{Type: "lines", Path: filepath.Join(home, "history.jsonl"), ID: n.ID})
	default:
		return nil, fmt.Errorf("Crush does not know where %s keeps its chats, so it cannot lock this one", n.Kind) //nolint:staticcheck
	}
	return items, nil
}

// fileHas reports whether a file holds text.
func fileHas(path, text string) bool {
	data, err := os.ReadFile(path)
	return err == nil && bytes.Contains(data, []byte(text))
}

// needTables refuses a CLI database without the tables Crush knows.
func needTables(path string, tables ...string) error {
	conn, err := db.OpenForeign(path)
	if err != nil {
		return err
	}
	defer conn.Close()
	for _, table := range tables {
		var n int
		if err := conn.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("%s is not in a format Crush knows (no table %s), so it cannot lock this chat", path, table)
		}
	}
	return nil
}

// -- lines files --

// splitLines splits a JSON-lines file into the lines holding id and the rest.
func splitLines(data []byte, ids []string) (keep, moved []byte) {
	for line := range bytes.SplitAfterSeq(data, []byte("\n")) {
		hit := false
		for _, id := range ids {
			if id != "" && bytes.Contains(line, []byte(id)) {
				hit = true
				break
			}
		}
		if hit {
			moved = append(moved, line...)
		} else {
			keep = append(keep, line...)
		}
	}
	return keep, moved
}

func (r entryRef) ids() []string {
	return append([]string{r.ID}, r.IDs...)
}

// -- SQLite stores --

// matchClause is the WHERE clause picking a table's rows of r, or "" when
// the table has none of r's columns.
func (r entryRef) matchClause(conn *sql.DB, schema, table string) (string, []any, error) {
	cols, err := tableColumns(conn, schema, table)
	if err != nil {
		return "", nil, err
	}
	var parts []string
	var args []any
	for _, c := range cols {
		if slices.Contains(r.Columns, c) || (c == "id" && slices.Contains(r.Primary, table)) {
			parts = append(parts, quote(c)+" = ?")
			args = append(args, r.ID)
		}
	}
	if len(parts) == 0 {
		return "", nil, nil
	}
	return strings.Join(parts, " OR "), args, nil
}

func quote(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

func tableColumns(conn interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, schema, table string,
) ([]string, error) {
	rows, err := conn.QueryContext(context.Background(), `SELECT name FROM pragma_table_info(?, ?)`, table, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

// plainTables lists a database's ordinary tables (no virtual tables,
// FTS shadow tables or SQLite's own).
func plainTables(conn *sql.DB, schema string) ([]string, error) {
	rows, err := conn.Query(`SELECT name, sql FROM ` + schema + `.sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		var create sql.NullString
		if err := rows.Scan(&name, &create); err != nil {
			return nil, err
		}
		if strings.HasPrefix(strings.ToUpper(create.String), "CREATE VIRTUAL") {
			continue
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// copySQLiteRows copies r's rows into a new SQLite file at dst, one table
// per source table that has any. It returns how many rows it copied.
func copySQLiteRows(ctx context.Context, r entryRef, dst string) (int64, error) {
	conn, err := db.OpenForeign(r.Path)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return 0, err
	}
	_ = os.Remove(dst)
	if _, err := conn.ExecContext(ctx, `ATTACH DATABASE ? AS copy`, dst); err != nil {
		return 0, err
	}
	defer conn.ExecContext(context.Background(), `DETACH DATABASE copy`) //nolint:errcheck
	tables, err := plainTables(conn, "main")
	if err != nil {
		return 0, err
	}
	var total int64
	for _, table := range tables {
		where, args, err := r.matchClause(conn, "main", table)
		if err != nil {
			return 0, err
		}
		if where == "" {
			continue
		}
		var n int64
		if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM main.`+quote(table)+` WHERE `+where, args...).Scan(&n); err != nil {
			return 0, err
		}
		if n == 0 {
			continue
		}
		// The copy needs no keys or references of its own.
		if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS copy.`+quote(table)+` AS SELECT * FROM main.`+quote(table)+` WHERE 0`); err != nil {
			return 0, err
		}
		if _, err := conn.ExecContext(ctx, `INSERT INTO copy.`+quote(table)+` SELECT * FROM main.`+quote(table)+` WHERE `+where, args...); err != nil {
			return 0, err
		}
		total += n
	}
	return total, nil
}

// deleteSQLiteRows removes r's rows (children first, then the primary
// tables), runs the cleanups After names, rebuilds the database and
// checkpoints the log, so no copy of the rows stays in either. The rebuild
// also clears space the CLI freed earlier without wiping it (leak test,
// 2026-11-03; about 4 s for a 1 GB OpenCode database).
func deleteSQLiteRows(ctx context.Context, r entryRef) error {
	conn, err := db.OpenForeign(r.Path)
	if err != nil {
		return err
	}
	defer conn.Close()
	tables, err := plainTables(conn, "main")
	if err != nil {
		return err
	}
	slices.SortStableFunc(tables, func(a, b string) int {
		ap, bp := slices.Contains(r.Primary, a), slices.Contains(r.Primary, b)
		switch {
		case ap == bp:
			return 0
		case ap:
			return 1
		default:
			return -1
		}
	})
	// Read before the transaction: the connection is the only one.
	type deletion struct {
		query string
		args  []any
	}
	var deletions []deletion
	for _, table := range tables {
		where, args, err := r.matchClause(conn, "main", table)
		if err != nil {
			return err
		}
		if where != "" {
			deletions = append(deletions, deletion{`DELETE FROM main.` + quote(table) + ` WHERE ` + where, args})
		}
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	for _, d := range deletions {
		if _, err := tx.ExecContext(ctx, d.query, d.args...); err != nil {
			return err
		}
	}
	for _, after := range r.After {
		if table, ok := strings.CutPrefix(after, "fts-optimize:"); ok {
			// Merges the full-text index, so deleted words leave it too.
			if _, err := tx.ExecContext(ctx, `INSERT INTO `+quote(table)+`(`+quote(table)+`) VALUES('optimize')`); err != nil {
				return err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `VACUUM`); err != nil {
		return err
	}
	return checkpoint(ctx, conn)
}

// checkpoint empties a database's write-ahead log, retrying while a
// reader holds it (PLAN §6: a running CLI can block it).
func checkpoint(ctx context.Context, conn interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
},
) error {
	var busy, logged, done int
	for range 20 {
		if err := conn.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logged, &done); err != nil {
			return err
		}
		if busy == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-sleepChan():
		}
	}
	return errors.New("another program kept the database busy; its log still holds deleted rows")
}

// restoreSQLiteRows puts rows saved by copySQLiteRows back, keeping rows
// that are there already.
func restoreSQLiteRows(ctx context.Context, r entryRef, src string) error {
	if _, err := os.Stat(src); err != nil {
		return err
	}
	conn, err := db.OpenForeign(r.Path)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `ATTACH DATABASE ? AS copy`, src); err != nil {
		return err
	}
	defer conn.ExecContext(context.Background(), `DETACH DATABASE copy`) //nolint:errcheck
	tables, err := plainTables(conn, "copy")
	if err != nil {
		return err
	}
	// Primary tables first, so their children's references hold.
	slices.SortStableFunc(tables, func(a, b string) int {
		ap, bp := slices.Contains(r.Primary, a), slices.Contains(r.Primary, b)
		switch {
		case ap == bp:
			return 0
		case ap:
			return -1
		default:
			return 1
		}
	})
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	for _, table := range tables {
		have, err := tableColumns(tx, "main", table)
		if err != nil {
			return err
		}
		saved, err := tableColumns(tx, "copy", table)
		if err != nil {
			return err
		}
		var cols []string
		for _, c := range saved {
			if slices.Contains(have, c) {
				cols = append(cols, quote(c))
			}
		}
		if len(cols) == 0 {
			continue
		}
		list := strings.Join(cols, ", ")
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO main.`+quote(table)+` (`+list+`) SELECT `+list+` FROM copy.`+quote(table)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// -- protobuf files --

// splitProto splits a file of repeated top-level entries (field 1, each a
// message whose own field 1 is its conversation ID) into the entries keyed
// by id and the rest. Any other shape is an unknown format.
func splitProto(data []byte, id string) (keep, moved []byte, err error) {
	rest := data
	for len(rest) > 0 {
		num, typ, n := protowire.ConsumeTag(rest)
		if n < 0 || num != 1 || typ != protowire.BytesType {
			return nil, nil, errors.New("unknown format")
		}
		body, m := protowire.ConsumeBytes(rest[n:])
		if m < 0 {
			return nil, nil, errors.New("unknown format")
		}
		entry := rest[:n+m]
		rest = rest[n+m:]
		if protoKey(body) == id {
			moved = append(moved, entry...)
		} else {
			keep = append(keep, entry...)
		}
	}
	return keep, moved, nil
}

// protoKey is an entry's field 1 as text, or "".
func protoKey(body []byte) string {
	num, typ, n := protowire.ConsumeTag(body)
	if n < 0 || num != 1 || typ != protowire.BytesType {
		return ""
	}
	key, m := protowire.ConsumeBytes(body[n:])
	if m < 0 {
		return ""
	}
	return string(key)
}

// -- copying out, removing and putting back --

// copyEntry saves r's entries in the vault at dst.
func copyEntry(ctx context.Context, r entryRef, dst string) error {
	switch r.Type {
	case "lines":
		data, err := os.ReadFile(r.Path)
		if err != nil {
			return err
		}
		_, moved := splitLines(data, r.ids())
		return writeFileSync(dst, moved)
	case "proto":
		data, err := os.ReadFile(r.Path)
		if err != nil {
			return err
		}
		_, moved, err := splitProto(data, r.ID)
		if err != nil {
			return fmt.Errorf("%s is not in a format Crush knows, so it cannot lock this chat", r.Path)
		}
		return writeFileSync(dst, moved)
	case "sqlite":
		_, err := copySQLiteRows(ctx, r, dst)
		if err != nil {
			return err
		}
		if _, err := os.Stat(dst); errors.Is(err, fs.ErrNotExist) {
			return writeFileSync(dst, nil) // nothing to save
		}
		return syncPath(dst)
	}
	return copyCrushEntry(r, dst)
}

// removeEntry removes r's entries from the shared file or database. It is
// safe to run again after a crash.
func removeEntry(ctx context.Context, r entryRef) error {
	switch r.Type {
	case "lines":
		return rewriteShared(r.Path, func(data []byte) ([]byte, bool, error) {
			keep, moved := splitLines(data, r.ids())
			return keep, len(moved) > 0, nil
		})
	case "proto":
		return rewriteShared(r.Path, func(data []byte) ([]byte, bool, error) {
			keep, moved, err := splitProto(data, r.ID)
			if err != nil {
				return nil, false, fmt.Errorf("%s: %w", r.Path, err)
			}
			return keep, len(moved) > 0, nil
		})
	case "sqlite":
		if _, err := os.Stat(r.Path); errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return deleteSQLiteRows(ctx, r)
	}
	return removeCrushEntry(r)
}

// restoreEntry puts entries saved at src back into the shared store.
func restoreEntry(ctx context.Context, r entryRef, src string) error {
	switch r.Type {
	case "lines", "proto":
		saved, err := os.ReadFile(src)
		if err != nil || len(saved) == 0 {
			return err
		}
		return rewriteSharedOrCreate(r.Path, func(data []byte) ([]byte, bool, error) {
			if r.Type == "lines" {
				_, there := splitLines(data, r.ids())
				if len(there) > 0 {
					return nil, false, nil
				}
				if len(data) > 0 && !bytes.HasSuffix(data, []byte("\n")) {
					data = append(data, '\n')
				}
			} else if _, there, err := splitProto(data, r.ID); err != nil || len(there) > 0 {
				return nil, false, err
			}
			return append(slices.Clip(data), saved...), true, nil
		})
	case "sqlite":
		return restoreSQLiteRows(ctx, r, src)
	}
	return restoreCrushEntry(r, src)
}

// rewriteSharedOrCreate is rewriteShared that makes a missing file.
func rewriteSharedOrCreate(path string, change func([]byte) ([]byte, bool, error)) error {
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		next, ok, err := change(nil)
		if err != nil || !ok {
			return err
		}
		return writeFileSync(path, next)
	}
	return rewriteShared(path, change)
}

func syncPath(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Sync(); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

// marshalRef is an entry's pending-wipe target.
func marshalRef(r entryRef) string {
	data, _ := json.Marshal(r)
	return string(data)
}
