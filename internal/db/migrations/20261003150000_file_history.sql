-- +goose Up
CREATE TABLE file_history_changes (
 message_id TEXT NOT NULL,
 session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 tool_id TEXT NOT NULL,
 path TEXT NOT NULL,
 capture_order INTEGER NOT NULL,
 before_state TEXT NOT NULL,
 after_state TEXT NOT NULL,
 git_head TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(message_id,tool_id,path,capture_order)
);
CREATE INDEX file_history_session ON file_history_changes(session_id);
CREATE TABLE file_history_objects (digest TEXT PRIMARY KEY, size INTEGER NOT NULL);
CREATE TABLE file_history_undo (
 id TEXT PRIMARY KEY,
 session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 created_at INTEGER NOT NULL,
 entries TEXT NOT NULL
);
CREATE TABLE file_history_points (
 message_id TEXT PRIMARY KEY,
 session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 git_root TEXT NOT NULL,
 git_head TEXT NOT NULL
);
-- +goose StatementBegin
CREATE TRIGGER file_history_message_deleted AFTER DELETE ON messages BEGIN
 DELETE FROM file_history_changes WHERE message_id=OLD.id;
 DELETE FROM file_history_points WHERE message_id=OLD.id;
END;
-- +goose StatementEnd

-- +goose Down
-- No destructive downgrade.
