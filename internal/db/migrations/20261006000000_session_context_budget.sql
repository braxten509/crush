-- +goose Up
CREATE TABLE session_context_budgets (
    session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    token_limit INTEGER NOT NULL,
    estimated INTEGER NOT NULL
);

-- +goose Down
DROP TABLE session_context_budgets;
