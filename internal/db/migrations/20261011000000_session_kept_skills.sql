-- +goose Up
CREATE TABLE session_kept_skills (
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    skill_id TEXT NOT NULL,
    name TEXT NOT NULL,
    position INTEGER NOT NULL,
    -- A skill turned off stays listed so agent CLIs, which keep their own
    -- history, can be told it is off even after Crush restarts.
    turned_off INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (session_id, skill_id)
);

-- +goose Down
DROP TABLE session_kept_skills;
