-- +goose Up
-- Locked chats (owner, 2026-11-03): a locked chat's messages live only in
-- the Plasma Vault; its row stays here so the list shows its real title with
-- a lock. lock_state is '' (a normal chat), 'locking', 'locked',
-- 'unlocking', 'unlocked' (copied back while the vault is open) or 'relock'
-- (a plain copy is still here after a crash: hidden until it is back in the
-- vault). lock_phase is the step a lock or unlock reached, so a crash can be
-- finished or undone without ever wiping the only copy. Neither column is in
-- update_sessions_updated_at's list, so locking never moves the chat.
ALTER TABLE sessions ADD COLUMN lock_state TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN lock_phase TEXT NOT NULL DEFAULT '';

-- What a lock or unlock still has to wipe once the vault copy is safe: kind
-- 'file' (a path), 'dir' (a folder) or 'entry' (an entry in a shared file or
-- an agent CLI's database). A crash finishes them on the next start without
-- the vault.
CREATE TABLE lock_pending_wipes (
    session_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    target TEXT NOT NULL,
    PRIMARY KEY (session_id, kind, target)
);

-- +goose Down
DROP TABLE lock_pending_wipes;
ALTER TABLE sessions DROP COLUMN lock_phase;
ALTER TABLE sessions DROP COLUMN lock_state;
