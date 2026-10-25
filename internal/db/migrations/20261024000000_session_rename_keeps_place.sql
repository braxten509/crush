-- +goose Up
-- Renaming a chat must not move it to the top of the list: the trigger now
-- stamps updated_at only when something other than the title changes. An
-- UPDATE that sets none of these columns (a rename, or an explicit
-- updated_at) leaves the time alone. A column added to sessions later that
-- should move the chat belongs in this list.
-- +goose StatementBegin
DROP TRIGGER IF EXISTS update_sessions_updated_at;
CREATE TRIGGER update_sessions_updated_at
AFTER UPDATE OF parent_session_id, message_count, prompt_tokens, completion_tokens, cost, summary_message_id, todos, channel ON sessions
BEGIN
UPDATE sessions SET updated_at = strftime('%s', 'now')
WHERE id = new.id;
END;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS update_sessions_updated_at;
CREATE TRIGGER update_sessions_updated_at
AFTER UPDATE ON sessions
BEGIN
UPDATE sessions SET updated_at = strftime('%s', 'now')
WHERE id = new.id;
END;
-- +goose StatementEnd
