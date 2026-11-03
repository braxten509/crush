-- name: GetMessage :one
SELECT *
FROM messages
WHERE id = ? LIMIT 1;

-- name: ListMessagesBySession :many
SELECT *
FROM messages
WHERE session_id = ?
ORDER BY created_at ASC;

-- name: ListMessagesBySessionFromSummary :many
-- Messages from the summary onward, which is all a compacted session sends.
-- created_at has one-second resolution, so a few messages preceding the
-- summary can come back too; the caller slices from the summary by ID.
SELECT m.*
FROM messages m
WHERE m.session_id = ?
  AND m.created_at >= (SELECT s.created_at FROM messages s WHERE s.id = ?)
ORDER BY m.created_at ASC;

-- name: CreateMessage :one
INSERT INTO messages (
    id,
    session_id,
    role,
    parts,
    model,
    provider,
    is_summary_message,
    created_at,
    updated_at
)
SELECT
    ?, ?, ?, ?, ?, ?, ?, strftime('%s', 'now'), strftime('%s', 'now')
WHERE NOT EXISTS (
    WITH RECURSIVE ancestors(id, parent_session_id, lock_state) AS (
        SELECT id, parent_session_id, lock_state FROM sessions WHERE id = sqlc.arg(session_id)
        UNION
        SELECT s.id, s.parent_session_id, s.lock_state FROM sessions s JOIN ancestors a ON s.id = a.parent_session_id
    )
    SELECT 1 FROM ancestors WHERE lock_state IN ('locking', 'locked', 'unlocking', 'relock')
)
RETURNING *;

-- name: UpdateMessage :exec
UPDATE messages
SET
    parts = ?,
    prism_model_id = ?,
    prism_model_name = ?,
    prism_hypercredit_savings = ?,
    prism_dollar_savings = ?,
    finished_at = ?,
    updated_at = strftime('%s', 'now')
WHERE id = ?;


-- name: DeleteMessage :exec
DELETE FROM messages
WHERE id = ?;

-- name: DeleteSessionMessages :exec
DELETE FROM messages
WHERE session_id = ?;

-- name: ListUserMessagesBySession :many
-- Backs prompt history, which steps back one entry at a time.
SELECT *
FROM messages
WHERE session_id = ? AND role = 'user'
ORDER BY created_at DESC
LIMIT 200;

-- name: ListAllUserMessages :many
-- Backs prompt history when no session is open. Needs
-- idx_messages_role_created_at to seek rather than scan the table.
SELECT *
FROM messages
WHERE role = 'user'
ORDER BY created_at DESC
LIMIT 200;

-- name: GetLastAssistantMessageBySession :one
SELECT *
FROM messages
WHERE session_id = ? AND role = 'assistant' AND is_summary_message = 0
ORDER BY created_at DESC
LIMIT 1;
