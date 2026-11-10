-- +goose Up
-- The Git branch and hidden worktree folder a chat runs in (owner,
-- 2026-11-10: changing the working branch in one chat must not change it for
-- other chats in the folder). A chat without a row uses the folder's own
-- working tree and branch.
CREATE TABLE session_branches (
    session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
    branch TEXT NOT NULL,
    work_dir TEXT NOT NULL DEFAULT ''
);

-- +goose Down
DROP TABLE session_branches;
