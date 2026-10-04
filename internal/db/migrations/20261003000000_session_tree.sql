-- +goose Up
-- +goose StatementBegin
-- Side tables deliberately leave upstream's messages and sessions unchanged.
CREATE TABLE tree_nodes (
    message_id TEXT PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    parent_id TEXT,
    label TEXT NOT NULL DEFAULT ''
);
CREATE INDEX tree_nodes_session ON tree_nodes(session_id);
CREATE INDEX tree_nodes_parent ON tree_nodes(parent_id);
CREATE TABLE tree_heads (
    session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
    message_id TEXT,
    revision INTEGER NOT NULL DEFAULT 0
);
-- rowid breaks ties in the old one-second timestamps by insertion order.
INSERT INTO tree_nodes(message_id, session_id, parent_id)
SELECT id, session_id, lag(id) OVER (PARTITION BY session_id ORDER BY created_at, rowid)
FROM messages;
INSERT INTO tree_heads(session_id, message_id)
SELECT session_id, id FROM (
    SELECT id, session_id, row_number() OVER (PARTITION BY session_id ORDER BY created_at DESC, rowid DESC) AS position FROM messages
) WHERE position = 1;
CREATE TRIGGER tree_append AFTER INSERT ON messages BEGIN
    INSERT INTO tree_nodes(message_id, session_id, parent_id)
    VALUES(new.id, new.session_id, (SELECT message_id FROM tree_heads WHERE session_id = new.session_id));
    INSERT INTO tree_heads(session_id, message_id) VALUES(new.session_id, new.id)
    ON CONFLICT(session_id) DO UPDATE SET message_id = excluded.message_id;
END;
-- Upstream removes failed summary placeholders. Preserve their descendants.
CREATE TRIGGER tree_remove BEFORE DELETE ON messages BEGIN
    UPDATE tree_heads SET message_id = (SELECT parent_id FROM tree_nodes WHERE message_id = old.id)
    WHERE session_id = old.session_id AND message_id = old.id;
    UPDATE tree_nodes SET parent_id = (SELECT parent_id FROM tree_nodes WHERE message_id = old.id)
    WHERE parent_id = old.id;
END;
-- +goose StatementEnd

-- +goose Down
-- Deliberately no destructive downgrade: old binaries ignore the extra tables.
