-- +goose Up
-- The model a chat runs on (owner, 2026-10-23: a model picked in one chat
-- must not reach another), kept as config.SelectedModel JSON so every
-- setting of it (effort, FAST, thinking, account) stays with the chat. A
-- chat without a row uses the workspace's model.
CREATE TABLE session_models (
    session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
    choice TEXT NOT NULL
);

-- +goose Down
DROP TABLE session_models;
