-- +goose Up
-- A link from highlighted words in a message to the branch chat they opened.
-- Each branch has exactly one origin link (copied_from IS NULL): its parent
-- chat and source message. Links inside a copied part of a chat come along
-- into the copy as extra rows with copied_from set, so sibling branches stay
-- reachable. copied_from has no foreign key on purpose: a copied link stays
-- a copy even after the link it came from is gone.
CREATE TABLE chat_branches (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    parent_session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    source_message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    highlight_text TEXT NOT NULL,
    -- Offsets in UTF-16 code units into the source message's text.
    highlight_start INTEGER NOT NULL,
    highlight_end INTEGER NOT NULL,
    copied_from TEXT,
    created_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX chat_branches_origin ON chat_branches(session_id) WHERE copied_from IS NULL;
CREATE INDEX chat_branches_parent ON chat_branches(parent_session_id);
CREATE INDEX chat_branches_message ON chat_branches(source_message_id);

-- Each AI tool's own ID for a Crush message, recorded as replies arrive, so
-- a branch can fork the tool's memory at the cut.
CREATE TABLE cli_message_markers (
    message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    cli TEXT NOT NULL,            -- catwalk type of the AI tool
    native_session TEXT NOT NULL, -- the tool's own session or thread ID
    marker TEXT NOT NULL,         -- the tool's own message ID, turn ID or prompt index
    marker_kind TEXT NOT NULL,    -- 'message' | 'turn' | 'prompt_index' | 'step'
    created_at INTEGER NOT NULL,
    PRIMARY KEY (message_id, cli)
);
CREATE INDEX cli_message_markers_session ON cli_message_markers(session_id, created_at);

-- +goose Down
DROP TABLE cli_message_markers;
DROP TABLE chat_branches;
