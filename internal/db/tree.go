package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type TreeNode struct {
	MessageID string
	ParentID  string
	Label     string
	Active    bool
}

func (q *Queries) TreeNodes(ctx context.Context, sessionID string) ([]TreeNode, error) {
	rows, err := q.db.QueryContext(ctx, `SELECT n.message_id, coalesce(n.parent_id,''), n.label,
 n.message_id = coalesce(h.message_id,'') FROM tree_nodes n
 JOIN messages m ON m.id=n.message_id LEFT JOIN tree_heads h ON h.session_id=n.session_id
 WHERE n.session_id=? ORDER BY m.created_at,m.rowid`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var nodes []TreeNode
	for rows.Next() {
		var n TreeNode
		if err := rows.Scan(&n.MessageID, &n.ParentID, &n.Label, &n.Active); err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

const treePath = `WITH RECURSIVE path(message_id,parent_id,depth) AS (
 SELECT n.message_id,n.parent_id,0 FROM tree_nodes n JOIN tree_heads h ON h.message_id=n.message_id WHERE h.session_id=?
 UNION ALL SELECT n.message_id,n.parent_id,p.depth+1 FROM tree_nodes n JOIN path p ON n.message_id=p.parent_id
) `

func (q *Queries) TreePath(ctx context.Context, sessionID string) ([]string, error) {
	rows, err := q.db.QueryContext(ctx, treePath+`SELECT message_id FROM path ORDER BY depth DESC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (q *Queries) TreeRevision(ctx context.Context, sessionID string) (int64, error) {
	var revision int64
	err := q.db.QueryRowContext(ctx, `SELECT revision FROM tree_heads WHERE session_id=?`, sessionID).Scan(&revision)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return revision, err
}

// TreeHead returns the message a chat's head is on: "" when the head is
// before the first message, or the chat has none.
func (q *Queries) TreeHead(ctx context.Context, sessionID string) (string, error) {
	var head sql.NullString
	err := q.db.QueryRowContext(ctx, `SELECT message_id FROM tree_heads WHERE session_id=?`, sessionID).Scan(&head)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return head.String, err
}

// SwitchTree requires an idle, exclusively owned workspace. One transaction
// changes both the cursor and compaction pointer; no transcript is rewritten.
func (q *Queries) SwitchTree(ctx context.Context, sessionID, messageID string) error {
	return q.SwitchTreeNote(ctx, sessionID, messageID, nil)
}

type TreeNote struct {
	Message            CreateMessageParams
	FromID, AncestorID string
}

// A note and the new cursor commit together; failure leaves the old path intact.
func (q *Queries) SwitchTreeNote(ctx context.Context, sessionID, messageID string, note *TreeNote) error {
	conn, ok := q.db.(*sql.DB)
	if !ok {
		return fmt.Errorf("tree switching requires a database connection")
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM tree_nodes WHERE session_id=? AND message_id=?`, sessionID, messageID).Scan(&exists); err != nil {
		return err
	}
	if messageID != "" && exists != 1 {
		return fmt.Errorf("tree entry does not belong to this session")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO tree_heads(session_id,message_id,revision) VALUES(?,?,1) ON CONFLICT(session_id) DO UPDATE SET message_id=excluded.message_id,revision=tree_heads.revision+1`, sessionID, sql.NullString{String: messageID, Valid: messageID != ""}); err != nil {
		return err
	}
	if note != nil {
		if _, err = New(tx).CreateMessage(ctx, note.Message); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO tree_branch_summaries(message_id,from_id,ancestor_id) VALUES(?,?,?)`, note.Message.ID, note.FromID, note.AncestorID); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, treePath+`UPDATE sessions SET summary_message_id=(SELECT m.id FROM path p JOIN messages m ON m.id=p.message_id WHERE m.is_summary_message=1 ORDER BY p.depth LIMIT 1), todos='[]' WHERE id=?`, sessionID, sessionID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (q *Queries) LabelTree(ctx context.Context, sessionID, messageID, label string) error {
	result, err := q.db.ExecContext(ctx, `UPDATE tree_nodes SET label=? WHERE session_id=? AND message_id=?`, label, sessionID, messageID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return fmt.Errorf("tree entry not found")
	}
	return err
}

// ListTreeMessages fetches only the selected ancestry, in path order.
func (q *Queries) ListTreeMessages(ctx context.Context, sessionID string) ([]Message, error) {
	query := strings.Replace(listMessagesBySession, "FROM messages", "FROM messages JOIN path ON path.message_id=messages.id", 1)
	query = strings.Replace(query, "ORDER BY created_at ASC", "ORDER BY path.depth DESC", 1)
	rows, err := q.db.QueryContext(ctx, treePath+query, sessionID, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Parts, &m.Model, &m.CreatedAt, &m.UpdatedAt, &m.FinishedAt, &m.Provider, &m.IsSummaryMessage, &m.PrismModelID, &m.PrismModelName, &m.PrismHypercreditSavings, &m.PrismDollarSavings); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

type TreePreview struct {
	TreeNode
	Role, Text string
}

// Keep topology, but never bring tool payloads, attachments or full transcripts
// into the selector. Each text preview is bounded to 240 characters in SQL.
func (q *Queries) TreePreviews(ctx context.Context, sessionID string) ([]TreePreview, error) {
	rows, err := q.db.QueryContext(ctx, `SELECT n.message_id,coalesce(n.parent_id,''),n.label,
 n.message_id=coalesce(h.message_id,''),m.role,
 coalesce((SELECT substr(json_extract(value,'$.data.text'),1,240) FROM json_each(m.parts) WHERE json_extract(value,'$.type')='text' LIMIT 1),'')
 FROM tree_nodes n JOIN messages m ON m.id=n.message_id LEFT JOIN tree_heads h ON h.session_id=n.session_id
 WHERE n.session_id=? ORDER BY m.created_at,m.rowid`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []TreePreview
	for rows.Next() {
		var p TreePreview
		if err := rows.Scan(&p.MessageID, &p.ParentID, &p.Label, &p.Active, &p.Role, &p.Text); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

// CopyTree copies one ancestry into a new top-level chat, in a transaction.
// Native CLI links are deliberately absent for the new session ID.
func (q *Queries) CopyTree(ctx context.Context, source, target, newID string) error {
	conn, ok := q.db.(*sql.DB)
	if !ok {
		return fmt.Errorf("tree copy requires a database connection")
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var title string
	if err = tx.QueryRowContext(ctx, `SELECT title FROM sessions WHERE id=?`, source).Scan(&title); err != nil {
		return err
	}
	if target != "" {
		var owner string
		if err = tx.QueryRowContext(ctx, `SELECT session_id FROM messages WHERE id=?`, target).Scan(&owner); err != nil {
			return err
		}
		if owner != source {
			return fmt.Errorf("entry belongs to another chat")
		}
	}
	if _, err = New(tx).CreateSession(ctx, CreateSessionParams{ID: newID, Title: title + " (copy)"}); err != nil {
		return err
	}
	ids, err := ancestry(ctx, tx, source, target)
	if err != nil {
		return err
	}
	if _, err = copyMessages(ctx, tx, newID, ids); err != nil {
		return err
	}
	if err = copyChatExtras(ctx, tx, source, newID); err != nil {
		return err
	}
	if err = copyTodos(ctx, tx, source, newID); err != nil {
		return err
	}
	if err = pointAtSummary(ctx, tx, newID); err != nil {
		return err
	}
	return tx.Commit()
}

// TreeAncestry lists messageID and its ancestors in a chat, from the root
// down to messageID. It is empty when messageID is not in the chat.
func (q *Queries) TreeAncestry(ctx context.Context, sessionID, messageID string) ([]string, error) {
	return ancestry(ctx, q.db, sessionID, messageID)
}

func ancestry(ctx context.Context, q DBTX, sessionID, messageID string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `WITH RECURSIVE path(id,parent,depth) AS (
 SELECT message_id,parent_id,0 FROM tree_nodes WHERE message_id=? AND session_id=?
 UNION ALL SELECT n.message_id,n.parent_id,p.depth+1 FROM tree_nodes n JOIN path p ON n.message_id=p.parent)
 SELECT id FROM path ORDER BY depth DESC`, messageID, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// copyMessages copies the messages ids, in path order, into the chat newID.
// Each insert passes through the tree_append trigger, which hangs it under
// the copy's head. Every copied message brings its price columns, file
// history, tree label, branch summary, review details, tool markers and
// links (engine-design.md 5.11). It returns the map from old to new
// message IDs.
func copyMessages(ctx context.Context, tx *sql.Tx, newID string, ids []string) (map[string]string, error) {
	insert, err := tx.PrepareContext(ctx, `INSERT INTO messages(id,session_id,role,parts,model,provider,is_summary_message,created_at,updated_at,finished_at,prism_model_id,prism_model_name,prism_hypercredit_savings,prism_dollar_savings)
 SELECT ?,?,role,parts,model,provider,is_summary_message,created_at,updated_at,finished_at,prism_model_id,prism_model_name,prism_hypercredit_savings,prism_dollar_savings FROM messages WHERE id=?`)
	if err != nil {
		return nil, err
	}
	defer insert.Close()
	copied := make(map[string]string, len(ids))
	for _, id := range ids {
		newMessageID := uuid.NewString()
		result, err := insert.ExecContext(ctx, newMessageID, newID, id)
		if err != nil {
			return nil, err
		}
		if n, err := result.RowsAffected(); err != nil || n != 1 {
			return nil, fmt.Errorf("message %s is gone", id)
		}
		copied[id] = newMessageID
	}
	// The extras are copied set-wise, keeping the transaction short: the
	// copy holds the one database connection a running reply also writes
	// through. json_each over the map yields the old ID as key and the new
	// ID as value.
	pairs, err := json.Marshal(copied)
	if err != nil {
		return nil, err
	}
	for _, statement := range []string{
		`INSERT INTO file_history_points(message_id,session_id,git_root,git_head)
 SELECT j.value,?,p.git_root,p.git_head FROM json_each(?) j JOIN file_history_points p ON p.message_id=j.key`,
		`INSERT INTO file_history_changes(message_id,session_id,tool_id,path,capture_order,before_state,after_state,git_head)
 SELECT j.value,?,c.tool_id,c.path,c.capture_order,c.before_state,c.after_state,c.git_head FROM json_each(?) j JOIN file_history_changes c ON c.message_id=j.key`,
		`INSERT INTO cli_message_markers(message_id,session_id,cli,native_session,marker,marker_kind,created_at)
 SELECT j.value,?,m.cli,m.native_session,m.marker,m.marker_kind,m.created_at FROM json_each(?) j JOIN cli_message_markers m ON m.message_id=j.key`,
	} {
		if _, err := tx.ExecContext(ctx, statement, newID, string(pairs)); err != nil {
			return nil, err
		}
	}
	for _, statement := range []string{
		`UPDATE tree_nodes SET label=o.label FROM (SELECT j.value AS id,n.label FROM json_each(?) j JOIN tree_nodes n ON n.message_id=j.key WHERE n.label!='') o WHERE tree_nodes.message_id=o.id`,
		`INSERT INTO tree_branch_summaries(message_id,from_id,ancestor_id)
 SELECT j.value,s.from_id,s.ancestor_id FROM json_each(?) j JOIN tree_branch_summaries s ON s.message_id=j.key`,
		`INSERT INTO message_review_details(message_id,payload)
 SELECT j.value,r.payload FROM json_each(?) j JOIN message_review_details r ON r.message_id=j.key`,
	} {
		if _, err := tx.ExecContext(ctx, statement, string(pairs)); err != nil {
			return nil, err
		}
	}
	if err := copyLinks(ctx, tx, newID, string(pairs)); err != nil {
		return nil, err
	}
	return copied, nil
}

// copyLinks copies the links shown on the copied messages. Each copy opens
// the same branch from the new chat's message and records the row it came
// from, so sibling branches stay reachable from the copy.
func copyLinks(ctx context.Context, tx *sql.Tx, newID, pairs string) error {
	rows, err := tx.QueryContext(ctx, `SELECT c.id,c.session_id,j.value,c.highlight_text,c.highlight_start,c.highlight_end
 FROM json_each(?) j JOIN chat_branches c ON c.source_message_id=j.key ORDER BY c.created_at,c.rowid`, pairs)
	if err != nil {
		return err
	}
	var links []ChatLink
	for rows.Next() {
		var l ChatLink
		if err := rows.Scan(&l.CopiedFrom, &l.SessionID, &l.SourceMessageID, &l.HighlightText, &l.HighlightStart, &l.HighlightEnd); err != nil {
			rows.Close()
			return err
		}
		links = append(links, l)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	for _, l := range links {
		l.ID, l.ParentSessionID, l.CreatedAt = uuid.NewString(), newID, now
		if err := insertLink(ctx, tx, l); err != nil {
			return err
		}
	}
	return nil
}

// copyChatExtras copies what belongs to the whole chat: kept skills, the
// context budget and read-file records. Cost and token totals stay 0, so
// usage reports never count the source twice.
func copyChatExtras(ctx context.Context, tx *sql.Tx, source, newID string) error {
	for _, statement := range []string{
		`INSERT INTO session_kept_skills(session_id,skill_id,name,position,turned_off,by_agent)
 SELECT ?,skill_id,name,position,turned_off,by_agent FROM session_kept_skills WHERE session_id=?`,
		`INSERT INTO session_context_budgets(session_id,provider,model,token_limit,estimated)
 SELECT ?,provider,model,token_limit,estimated FROM session_context_budgets WHERE session_id=?`,
		`INSERT INTO read_files(session_id,path,read_at) SELECT ?,path,read_at FROM read_files WHERE session_id=?`,
		// The copy runs on the chat's own model (owner, 2026-10-24).
		`INSERT INTO session_models(session_id,choice) SELECT ?,choice FROM session_models WHERE session_id=?`,
	} {
		if _, err := tx.ExecContext(ctx, statement, newID, source); err != nil {
			return err
		}
	}
	return nil
}

// copyTodos copies the chat's to-do list. Fork and clone carry it on; a
// branch, a sub-chat, starts with none (owner, 2026-10-21).
func copyTodos(ctx context.Context, tx *sql.Tx, source, newID string) error {
	_, err := tx.ExecContext(ctx, `UPDATE sessions SET todos=(SELECT todos FROM sessions WHERE id=?) WHERE id=?`, source, newID)
	return err
}

// pointAtSummary points a copied chat at the newest summary on its path.
func pointAtSummary(ctx context.Context, tx *sql.Tx, sessionID string) error {
	_, err := tx.ExecContext(ctx, treePath+`UPDATE sessions SET summary_message_id=(SELECT m.id FROM path p JOIN messages m ON m.id=p.message_id WHERE m.is_summary_message=1 ORDER BY p.depth LIMIT 1) WHERE id=?`, sessionID, sessionID)
	return err
}
