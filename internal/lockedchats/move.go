package lockedchats

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/crush/internal/session"
)

// A chat's rows in crush.db (PLAN §1), in the order they can be inserted.
// where picks the rows of the chats in temp.lock_set from the schema named
// by %[1]s.
type chatTable struct {
	name, where string
	// replace: rows the main database's triggers already made on insert
	// (tree rows) are replaced by the saved ones.
	replace bool
}

// in is the table's where clause for the schema.
func (t chatTable) in(schema string) string {
	return strings.ReplaceAll(t.where, "%[1]s", schema)
}

const inSet = "session_id IN (SELECT id FROM temp.lock_set)"

const ofSetMessages = "message_id IN (SELECT id FROM %[1]s.messages WHERE session_id IN (SELECT id FROM temp.lock_set))"

var chatTables = []chatTable{
	{name: "sessions", where: "id IN (SELECT id FROM temp.lock_set)"},
	{name: "messages", where: inSet},
	{name: "files", where: inSet},
	{name: "read_files", where: inSet},
	{name: "message_review_details", where: ofSetMessages},
	{name: "message_review_migrations", where: inSet},
	{name: "tree_nodes", where: inSet, replace: true},
	{name: "tree_heads", where: inSet, replace: true},
	{name: "tree_branch_summaries", where: ofSetMessages},
	// Branch links with both ends in the set; links to other chats are kept
	// as plain data (PLAN §9.2).
	{name: "chat_branches", where: inSet + " AND parent_session_id IN (SELECT id FROM temp.lock_set)"},
	{name: "cli_message_markers", where: inSet},
	{name: "session_context_budgets", where: inSet},
	{name: "session_kept_skills", where: inSet},
	{name: "session_models", where: inSet},
	{name: "file_history_changes", where: inSet},
	{name: "file_history_undo", where: inSet},
	{name: "file_history_points", where: inSet},
	{name: "file_history_objects", where: "digest IN (SELECT digest FROM temp.lock_digests)"},
}

// Tables with chat rows that are not the chat's own data, or are not chat
// data at all.
var otherTables = []string{"lock_pending_wipes", "goose_db_version", "crush_tree_schema_versions", "mcp_disabled_servers", "mcp_enabled_servers"}

// vaultTriggers are the vault copy's triggers; rows go in as they are.
var vaultTriggers = []string{
	"update_sessions_updated_at", "update_files_updated_at", "update_messages_updated_at",
	"update_session_message_count_on_insert", "update_session_message_count_on_delete",
	"tree_append", "tree_remove", "file_history_message_deleted",
}

// digestsQuery lists the saved-file digests the chats in the set use (or,
// with NOT, the other chats use).
const digestsQuery = `SELECT json_extract(before_state,'$.digest') FROM main.file_history_changes WHERE %[1]s AND json_extract(before_state,'$.saved')=1
 UNION SELECT json_extract(after_state,'$.digest') FROM main.file_history_changes WHERE %[1]s AND json_extract(after_state,'$.saved')=1
 UNION SELECT json_extract(j.value,'$.before.digest') FROM main.file_history_undo u, json_each(u.entries) j WHERE u.%[1]s AND json_extract(j.value,'$.before.saved')=1
 UNION SELECT json_extract(j.value,'$.after.digest') FROM main.file_history_undo u, json_each(u.entries) j WHERE u.%[1]s AND json_extract(j.value,'$.after.saved')=1`

// checkSchema refuses to lock when crush.db has a table with chat rows
// this code doesn't move (a later migration): fail closed.
func checkSchema(ctx context.Context, conn *sql.Conn) error {
	rows, err := conn.QueryContext(ctx, `SELECT name FROM main.sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return err
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		tables = append(tables, name)
	}
	rows.Close()
	known := map[string]bool{}
	for _, t := range chatTables {
		known[t.name] = true
	}
	for _, t := range otherTables {
		known[t] = true
	}
	for _, table := range tables {
		if known[table] {
			continue
		}
		cols, err := tableColumns(conn, "main", table)
		if err != nil {
			return err
		}
		if slices.Contains(cols, "session_id") || slices.Contains(cols, "message_id") {
			return fmt.Errorf("this Crush does not know how to move the table %s yet, so it cannot lock the chat", table)
		}
	}
	return nil
}

// setLockSet fills temp.lock_set with the chats and temp.lock_digests with
// the saved-file digests they use.
func setLockSet(ctx context.Context, conn *sql.Conn, ids []string) error {
	stmts := []string{
		`CREATE TEMP TABLE IF NOT EXISTS lock_set(id TEXT PRIMARY KEY)`,
		`CREATE TEMP TABLE IF NOT EXISTS lock_digests(digest TEXT PRIMARY KEY)`,
		`DELETE FROM temp.lock_set`,
		`DELETE FROM temp.lock_digests`,
	}
	for _, stmt := range stmts {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	for _, id := range ids {
		if _, err := conn.ExecContext(ctx, `INSERT OR IGNORE INTO temp.lock_set(id) VALUES (?)`, id); err != nil {
			return err
		}
	}
	_, err := conn.ExecContext(ctx, `INSERT OR IGNORE INTO temp.lock_digests(digest) SELECT * FROM (`+fmt.Sprintf(digestsQuery, inSet)+`) WHERE 1`)
	return err
}

func dropLockSet(conn *sql.Conn) {
	ctx := context.Background()
	_, _ = conn.ExecContext(ctx, `DROP TABLE IF EXISTS temp.lock_set`)
	_, _ = conn.ExecContext(ctx, `DROP TABLE IF EXISTS temp.lock_digests`)
}

// exclusiveDigests are the set's saved-file digests no other chat uses:
// their files may be wiped.
func exclusiveDigests(ctx context.Context, conn *sql.Conn) ([]string, error) {
	others := "session_id NOT IN (SELECT id FROM temp.lock_set)"
	return queryStrings(ctx, conn, `SELECT digest FROM temp.lock_digests WHERE digest NOT IN (`+fmt.Sprintf(digestsQuery, others)+`)`)
}

func queryStrings(ctx context.Context, conn interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, query string, args ...any,
) ([]string, error) {
	rows, err := conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s sql.NullString
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		if s.Valid {
			out = append(out, s.String)
		}
	}
	return out, rows.Err()
}

// sharedColumns are the columns a table has in both schemas, quoted.
func sharedColumns(ctx context.Context, conn *sql.Conn, table, a, b string) (string, error) {
	have, err := tableColumns(conn, a, table)
	if err != nil {
		return "", err
	}
	other, err := tableColumns(conn, b, table)
	if err != nil {
		return "", err
	}
	var cols []string
	for _, c := range have {
		if slices.Contains(other, c) {
			cols = append(cols, quote(c))
		}
	}
	if len(cols) == 0 {
		return "", fmt.Errorf("table %s has no columns in common", table)
	}
	return strings.Join(cols, ", "), nil
}

// allSharedColumns reads every chat table's shared columns, before a
// transaction starts on the connection.
func allSharedColumns(ctx context.Context, conn *sql.Conn, a, b string) (map[string]string, error) {
	columns := map[string]string{}
	for _, t := range chatTables {
		cols, err := sharedColumns(ctx, conn, t.name, a, b)
		if err != nil {
			return nil, err
		}
		columns[t.name] = cols
	}
	return columns, nil
}

// copyRowsOut copies the set's rows from main into the attached vault
// copy and returns each table's row count.
func copyRowsOut(ctx context.Context, conn *sql.Conn) (map[string]int64, error) {
	columns, err := allSharedColumns(ctx, conn, "main", "vault")
	if err != nil {
		return nil, err
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck
	for _, trigger := range vaultTriggers {
		if _, err := tx.ExecContext(ctx, `DROP TRIGGER IF EXISTS vault.`+trigger); err != nil {
			return nil, err
		}
	}
	counts := map[string]int64{}
	for _, t := range chatTables {
		cols := columns[t.name]
		res, err := tx.ExecContext(ctx, `INSERT INTO vault.`+t.name+` (`+cols+`) SELECT `+cols+` FROM main.`+t.name+` WHERE `+t.in("main"))
		if err != nil {
			return nil, fmt.Errorf("copying %s: %w", t.name, err)
		}
		counts[t.name], _ = res.RowsAffected()
	}
	return counts, tx.Commit()
}

// countRows counts the set's rows of every chat table in a schema.
func countRows(ctx context.Context, conn *sql.Conn, schema string) (map[string]int64, error) {
	counts := map[string]int64{}
	for _, t := range chatTables {
		var n int64
		if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM `+schema+`.`+t.name+` WHERE `+t.in(schema)).Scan(&n); err != nil {
			return nil, err
		}
		counts[t.name] = n
	}
	return counts, nil
}

// externalBranches are the branch links between a chat in the set and one
// outside it, as plain column values (PLAN §9.2).
func externalBranches(ctx context.Context, conn *sql.Conn) ([]map[string]any, error) {
	rows, err := conn.QueryContext(ctx, `SELECT * FROM main.chat_branches WHERE
	 (session_id IN (SELECT id FROM temp.lock_set)) != (parent_session_id IN (SELECT id FROM temp.lock_set))
	 OR (session_id NOT IN (SELECT id FROM temp.lock_set) AND source_message_id IN (SELECT id FROM main.messages WHERE `+inSet+`))`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := map[string]any{}
		for i, c := range cols {
			if b, ok := values[i].([]byte); ok {
				values[i] = string(b)
			}
			row[c] = values[i]
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// lockTx is the one transaction that takes the set's rows out of crush.db
// once the vault copy is complete: it deletes them, records what is left to
// wipe, marks the chat locked and puts its list time back last.
func lockTx(ctx context.Context, conn *sql.Conn, root string, updatedAt int64, digests []string, wipes []pendingWipe) error {
	columns, err := allSharedColumns(ctx, conn, "main", "vault")
	if err != nil {
		return err
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	// BEGIN IMMEDIATE owns SQLite's writer lock. Compare actual values in
	// both directions, including same-second updates and deleted rows.
	// Nothing may be deleted from a snapshot another process made stale.
	for _, t := range chatTables {
		cols := columns[t.name]
		for _, schemas := range [][2]string{{"main", "vault"}, {"vault", "main"}} {
			var changed bool
			query := `SELECT EXISTS(SELECT ` + cols + ` FROM ` + schemas[0] + `.` + t.name + ` WHERE ` + t.in(schemas[0]) +
				` EXCEPT SELECT ` + cols + ` FROM ` + schemas[1] + `.` + t.name + ` WHERE ` + t.in(schemas[1]) + `)`
			if err := tx.QueryRowContext(ctx, query).Scan(&changed); err != nil {
				return err
			}
			if changed {
				return fmt.Errorf("the chat changed during locking (%s); nothing was removed; try locking again", t.name)
			}
		}
	}
	var extraChild bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sessions WHERE parent_session_id IN (SELECT id FROM temp.lock_set) AND id NOT IN (SELECT id FROM temp.lock_set))`).Scan(&extraChild); err != nil {
		return err
	}
	if extraChild {
		return errors.New("the chat changed during locking (new sub-agent); nothing was removed; try locking again")
	}
	stmts := []string{
		`DELETE FROM main.messages WHERE ` + inSet,
		`DELETE FROM main.chat_branches WHERE ` + inSet + ` OR parent_session_id IN (SELECT id FROM temp.lock_set)`,
	}
	for _, t := range chatTables {
		switch t.name {
		case "sessions", "messages", "chat_branches", "file_history_objects", "message_review_details", "tree_branch_summaries":
			continue
		}
		stmts = append(stmts, `DELETE FROM main.`+t.name+` WHERE `+t.in("main"))
	}
	stmts = append(stmts, `DELETE FROM main.sessions WHERE id IN (SELECT id FROM temp.lock_set) AND id != '`+strings.ReplaceAll(root, "'", "''")+`'`)
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("%s: %w", stmt, err)
		}
	}
	for _, digest := range digests {
		if _, err := tx.ExecContext(ctx, `DELETE FROM main.file_history_objects WHERE digest = ?`, digest); err != nil {
			return err
		}
	}
	for _, w := range wipes {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO main.lock_pending_wipes(session_id, kind, target) VALUES (?, ?, ?)`, root, w.Kind, w.Target); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE main.sessions SET title = ?, todos = NULL, summary_message_id = NULL, lock_state = 'locking', lock_phase = 'wipe' WHERE id = ?`, session.LockedTitle, root); err != nil {
		return err
	}
	// Last, so no trigger moves the chat in the list (PLAN §9.7).
	if _, err := tx.ExecContext(ctx, `UPDATE main.sessions SET updated_at = ? WHERE id = ?`, updatedAt, root); err != nil {
		return err
	}
	return tx.Commit()
}

// unlockTx puts the vault copy's rows back into crush.db in one
// transaction with the chat's move to unlocked. It refuses (and changes
// nothing) when the result fails the foreign-key or tree checks.
func unlockTx(ctx context.Context, conn *sql.Conn, root string, branches []map[string]any, want map[string]int64) error {
	columns, err := allSharedColumns(ctx, conn, "vault", "main")
	if err != nil {
		return err
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	rootQ := "'" + strings.ReplaceAll(root, "'", "''") + "'"
	for _, t := range chatTables {
		cols := columns[t.name]
		where := t.in("vault")
		verb := "INSERT OR REPLACE"
		switch {
		case t.name == "sessions":
			verb, where = "INSERT", where+" AND id != "+rootQ
		case t.name == "messages":
			verb = "INSERT"
		case t.name == "file_history_objects":
			// Every saved version in the vault copy is the chat's; the
			// digest set was read before its rows came back.
			verb, where = "INSERT OR IGNORE", "1"
		}
		if _, err := tx.ExecContext(ctx, verb+` INTO main.`+t.name+` (`+cols+`) SELECT `+cols+` FROM vault.`+t.name+` WHERE `+where); err != nil {
			return fmt.Errorf("restoring %s: %w", t.name, err)
		}
	}
	for _, row := range branches {
		if err := restoreBranch(ctx, tx, row); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE main.sessions SET
		title = (SELECT v.title FROM vault.sessions v WHERE v.id = sessions.id),
		message_count = (SELECT v.message_count FROM vault.sessions v WHERE v.id = sessions.id),
		todos = (SELECT v.todos FROM vault.sessions v WHERE v.id = sessions.id),
		summary_message_id = (SELECT v.summary_message_id FROM vault.sessions v WHERE v.id = sessions.id)
		WHERE id IN (SELECT id FROM temp.lock_set) AND id IN (SELECT id FROM vault.sessions)`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE main.sessions SET lock_state = 'unlocked', lock_phase = '' WHERE id = ?`, root); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM main.lock_pending_wipes WHERE session_id = ?`, root); err != nil {
		return err
	}
	if err := checkRestored(ctx, tx, want); err != nil {
		return err
	}
	// Last, so the chat keeps its place in the list (PLAN §9.7).
	if _, err := tx.ExecContext(ctx, `UPDATE main.sessions SET updated_at = (SELECT v.updated_at FROM vault.sessions v WHERE v.id = sessions.id)
		WHERE id IN (SELECT id FROM temp.lock_set) AND id IN (SELECT id FROM vault.sessions)`); err != nil {
		return err
	}
	return tx.Commit()
}

// restoreBranch puts a link to a chat outside the set back when both its
// ends still exist.
func restoreBranch(ctx context.Context, tx *sql.Tx, row map[string]any) error {
	var ok bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM main.sessions WHERE id = ?)
		AND EXISTS(SELECT 1 FROM main.sessions WHERE id = ?)
		AND EXISTS(SELECT 1 FROM main.messages WHERE id = ?)`, row["session_id"], row["parent_session_id"], row["source_message_id"]).Scan(&ok)
	if err != nil || !ok {
		return err
	}
	cols := make([]string, 0, len(row))
	for c := range row {
		cols = append(cols, c)
	}
	slices.Sort(cols)
	quoted := make([]string, len(cols))
	args := make([]any, len(cols))
	for i, c := range cols {
		quoted[i] = quote(c)
		args[i] = row[c]
	}
	_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO main.chat_branches (`+strings.Join(quoted, ", ")+`) VALUES (?`+strings.Repeat(", ?", len(cols)-1)+`)`, args...)
	return err
}

// checkRestored refuses an unlock whose rows don't add up: foreign keys
// that point nowhere, tree rows without their message, or fewer rows than
// the vault copy holds (PLAN §9.7).
func checkRestored(ctx context.Context, tx *sql.Tx, want map[string]int64) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA main.foreign_key_check`)
	if err != nil {
		return err
	}
	bad := rows.Next()
	rows.Close()
	if bad {
		return errors.New("the restored chat failed the foreign key check")
	}
	var broken int64
	if err := tx.QueryRowContext(ctx, `SELECT
		(SELECT count(*) FROM main.tree_nodes WHERE `+inSet+` AND message_id NOT IN (SELECT id FROM main.messages))
		+ (SELECT count(*) FROM main.tree_heads WHERE `+inSet+` AND message_id IS NOT NULL AND message_id NOT IN (SELECT id FROM main.messages))`).Scan(&broken); err != nil {
		return err
	}
	if broken > 0 {
		return errors.New("the restored chat's branch tree is broken")
	}
	for _, t := range chatTables {
		var n int64
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM main.`+t.name+` WHERE `+t.in("main")).Scan(&n); err != nil {
			return err
		}
		if w, ok := want[t.name]; ok && n < w {
			return fmt.Errorf("the restored chat has %d rows of %s, the vault copy %d", n, t.name, w)
		}
	}
	return nil
}
