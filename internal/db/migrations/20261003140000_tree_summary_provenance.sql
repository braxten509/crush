-- +goose Up
CREATE TABLE tree_branch_summaries (
 message_id TEXT PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE,
 from_id TEXT NOT NULL,
 ancestor_id TEXT NOT NULL
);

-- +goose Down
-- No destructive downgrade.
