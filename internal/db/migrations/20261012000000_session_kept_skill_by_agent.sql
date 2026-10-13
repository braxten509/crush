-- +goose Up
-- Skills the agent turned on for a chat; it may turn only these off.
ALTER TABLE session_kept_skills ADD COLUMN by_agent INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE session_kept_skills DROP COLUMN by_agent;
