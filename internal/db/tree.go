package db

import (
	"context"
	"database/sql"
	"fmt"
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
	if exists != 1 {
		return fmt.Errorf("tree entry does not belong to this session")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tree_heads SET message_id=?,revision=revision+1 WHERE session_id=?`, messageID, sessionID); err != nil {
		return err
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
