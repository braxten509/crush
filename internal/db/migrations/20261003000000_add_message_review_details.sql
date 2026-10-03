-- +goose Up
CREATE TABLE message_review_details (
    message_id TEXT PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE,
    payload BLOB NOT NULL
);
CREATE TABLE message_review_migrations (
    session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE message_review_details;
DROP TABLE message_review_migrations;
