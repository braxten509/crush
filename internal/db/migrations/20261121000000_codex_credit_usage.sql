-- +goose Up
-- Each row holds the latest credit estimate for a native turn in a Crush chat.
CREATE TABLE codex_credit_usage (
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    thread_id TEXT NOT NULL,
    turn_id TEXT NOT NULL,
    estimated_credits REAL NOT NULL,
    PRIMARY KEY (session_id, thread_id, turn_id)
);

-- +goose Down
-- Older builds can ignore these totals.
