package db

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/google/uuid"
	"strings"
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
	rows, err := tx.QueryContext(ctx, `WITH RECURSIVE path(id,parent,depth) AS (
 SELECT message_id,parent_id,0 FROM tree_nodes WHERE message_id=? AND session_id=?
 UNION ALL SELECT n.message_id,n.parent_id,p.depth+1 FROM tree_nodes n JOIN path p ON n.message_id=p.parent)
 SELECT id FROM path ORDER BY depth DESC`, target, source)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		newMessageID := uuid.NewString()
		_, err = tx.ExecContext(ctx, `INSERT INTO messages(id,session_id,role,parts,model,provider,is_summary_message,created_at,updated_at,finished_at)
 SELECT ?,?,role,parts,model,provider,is_summary_message,created_at,updated_at,finished_at FROM messages WHERE id=?`, newMessageID, newID, id)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO file_history_points(message_id,session_id,git_root,git_head) SELECT ?,?,git_root,git_head FROM file_history_points WHERE message_id=?`, newMessageID, newID, id); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO file_history_changes(message_id,session_id,tool_id,path,capture_order,before_state,after_state,git_head) SELECT ?,?,tool_id,path,capture_order,before_state,after_state,git_head FROM file_history_changes WHERE message_id=?`, newMessageID, newID, id); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE tree_nodes SET label=(SELECT label FROM tree_nodes WHERE message_id=?) WHERE message_id=?`, id, newMessageID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO tree_branch_summaries(message_id,from_id,ancestor_id) SELECT ?,from_id,ancestor_id FROM tree_branch_summaries WHERE message_id=?`, newMessageID, id); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, treePath+`UPDATE sessions SET summary_message_id=(SELECT m.id FROM path p JOIN messages m ON m.id=p.message_id WHERE m.is_summary_message=1 ORDER BY p.depth LIMIT 1) WHERE id=?`, newID, newID)
	if err != nil {
		return err
	}
	return tx.Commit()
}
