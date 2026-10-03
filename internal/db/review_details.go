package db

import (
	"context"
	"database/sql"
)

// Review operations use the same transaction as their message. A saved
// summary can therefore never point at a half-written detail payload.
func (q *Queries) reviewTransaction(ctx context.Context, fn func(*Queries) error) error {
	if conn, ok := q.db.(*sql.DB); ok {
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err := fn(q.WithTx(tx)); err != nil {
			return err
		}
		return tx.Commit()
	}
	return fn(q)
}

// Reply order uses rowid: timestamps only have one-second precision.
const reviewCutoff = `COALESCE((SELECT rowid FROM messages
 WHERE session_id = ? AND role = 'assistant' AND is_summary_message = 0
 AND EXISTS (SELECT 1 FROM json_each(parts) p
   WHERE json_extract(p.value, '$.type') = 'text'
   AND trim(COALESCE(json_extract(p.value, '$.data.text'), '')) <> '')
 ORDER BY rowid DESC LIMIT 1 OFFSET 4), 0)`

func (q *Queries) putReview(ctx context.Context, id string, payload []byte) error {
	if len(payload) == 0 {
		return nil
	}
	var sessionID string
	if err := q.db.QueryRowContext(ctx, `SELECT session_id FROM messages WHERE id = ?`, id).Scan(&sessionID); err != nil {
		return err
	}
	root, err := q.reviewRoot(ctx, sessionID)
	if err != nil {
		return err
	}
	_, err = q.db.ExecContext(ctx, `INSERT INTO message_review_details(message_id, payload)
 SELECT id, ? FROM messages WHERE id = ? AND rowid >= MAX(`+reviewCutoff+`, `+reviewCutoff+`)
 ON CONFLICT(message_id) DO UPDATE SET payload = excluded.payload`, payload, id,
		sessionID, root)
	return err
}

func (q *Queries) reviewRoot(ctx context.Context, sessionID string) (string, error) {
	var root string
	err := q.db.QueryRowContext(ctx, `WITH RECURSIVE lineage(id, parent, depth) AS (
 SELECT id, parent_session_id, 0 FROM sessions WHERE id = ?
 UNION ALL SELECT s.id, s.parent_session_id, l.depth + 1 FROM sessions s JOIN lineage l ON s.id = l.parent WHERE l.depth < 64)
 SELECT id FROM lineage ORDER BY depth DESC LIMIT 1`, sessionID).Scan(&root)
	return root, err
}

func (q *Queries) CreateMessageWithReview(ctx context.Context, arg CreateMessageParams, payload []byte) (result Message, err error) {
	err = q.reviewTransaction(ctx, func(tx *Queries) error {
		var err error
		result, err = tx.CreateMessage(ctx, arg)
		if err != nil {
			return err
		}
		return tx.putReview(ctx, arg.ID, payload)
	})
	return
}

func (q *Queries) UpdateMessageWithReview(ctx context.Context, arg UpdateMessageParams, payload []byte) error {
	return q.reviewTransaction(ctx, func(tx *Queries) error {
		if err := tx.UpdateMessage(ctx, arg); err != nil {
			return err
		}
		return tx.putReview(ctx, arg.ID, payload)
	})
}

func (q *Queries) GetMessageReview(ctx context.Context, id string) ([]byte, error) {
	var payload []byte
	err := q.db.QueryRowContext(ctx, `SELECT payload FROM message_review_details WHERE message_id = ?`, id).Scan(&payload)
	return payload, err
}

func (q *Queries) PruneMessageReviews(ctx context.Context, sessionID string) error {
	root, err := q.reviewRoot(ctx, sessionID)
	if err != nil {
		return err
	}
	_, err = q.db.ExecContext(ctx, `WITH RECURSIVE family(id) AS (
 SELECT id FROM sessions WHERE id = ? UNION SELECT s.id FROM sessions s JOIN family f ON s.parent_session_id = f.id)
 DELETE FROM message_review_details WHERE message_id IN
 (SELECT m.id FROM messages m JOIN family f ON m.session_id = f.id WHERE m.rowid < `+reviewCutoff+`)`, root, root)
	if err != nil {
		return err
	}
	_, err = q.db.ExecContext(ctx, `DELETE FROM message_review_details WHERE message_id IN
 (SELECT id FROM messages WHERE session_id = ? AND rowid < `+reviewCutoff+`)`, sessionID, sessionID)
	return err
}

func (q *Queries) LegacyReviewMessages(ctx context.Context, sessionID string) ([]string, error) {
	var done int
	err := q.db.QueryRowContext(ctx, `SELECT 1 FROM message_review_migrations WHERE session_id = ?`, sessionID).Scan(&done)
	if err == nil {
		return nil, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	rows, err := q.db.QueryContext(ctx, `SELECT id FROM messages WHERE session_id = ? AND role = 'tool' ORDER BY rowid`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (q *Queries) ReviewCommands(ctx context.Context, sessionID string) (map[string]string, error) {
	rows, err := q.db.QueryContext(ctx, `SELECT json_extract(p.value, '$.data.id'), json_extract(p.value, '$.data.input')
 FROM messages m, json_each(m.parts) p WHERE m.session_id = ? AND m.role = 'assistant'
 AND json_extract(p.value, '$.type') = 'tool_call' AND json_extract(p.value, '$.data.name') = 'bash'`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	commands := map[string]string{}
	for rows.Next() {
		var id, input string
		if err := rows.Scan(&id, &input); err != nil {
			return nil, err
		}
		commands[id] = input
	}
	return commands, rows.Err()
}

func (q *Queries) MigrateMessageReview(ctx context.Context, id, original, compact string, payload []byte) error {
	return q.reviewTransaction(ctx, func(tx *Queries) error {
		result, err := tx.db.ExecContext(ctx, `UPDATE messages SET parts = ? WHERE id = ? AND parts = ?`, compact, id, original)
		if err != nil {
			return err
		}
		if count, _ := result.RowsAffected(); count == 0 {
			return nil
		}
		return tx.putReview(ctx, id, payload)
	})
}

func (q *Queries) FinishReviewMigration(ctx context.Context, sessionID string) error {
	_, err := q.db.ExecContext(ctx, `INSERT OR IGNORE INTO message_review_migrations(session_id) VALUES (?)`, sessionID)
	return err
}
