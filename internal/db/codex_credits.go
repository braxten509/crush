package db

import "context"

// RecordCodexCredits saves the latest estimate for one native turn. A
// repeated or stale update cannot add to the chat's total.
func (q *Queries) RecordCodexCredits(ctx context.Context, sessionID, threadID, turnID string, credits float64) error {
	_, err := q.db.ExecContext(ctx, `
		INSERT INTO codex_credit_usage (session_id, thread_id, turn_id, estimated_credits)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (session_id, thread_id, turn_id) DO UPDATE SET estimated_credits = excluded.estimated_credits
		WHERE excluded.estimated_credits > codex_credit_usage.estimated_credits
	`, sessionID, threadID, turnID, credits)
	return err
}

// CodexCreditsUsed totals only the native turns that belong to this chat.
func (q *Queries) CodexCreditsUsed(ctx context.Context, sessionID string) (float64, error) {
	var credits float64
	err := q.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(estimated_credits), 0)
		FROM codex_credit_usage WHERE session_id = ?
	`, sessionID).Scan(&credits)
	return credits, err
}
