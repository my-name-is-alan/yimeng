-- 0003: playback session cursor, album cover provenance, metadata enrichment log.

ALTER TABLE sessions ADD COLUMN current_idx INTEGER NOT NULL DEFAULT -1;  -- index into queue_items, -1 = none
ALTER TABLE sessions ADD COLUMN repeat_mode TEXT NOT NULL DEFAULT 'off' CHECK (repeat_mode IN ('off','all','one'));

ALTER TABLE albums ADD COLUMN cover_source TEXT;   -- local | remote (e.g. Cover Art Archive)

CREATE TABLE metadata_enrichment (
    entity_type  TEXT NOT NULL,      -- track | album | artist
    entity_id    TEXT NOT NULL,
    provider     TEXT NOT NULL,      -- musicbrainz | ...
    status       TEXT NOT NULL CHECK (status IN ('matched','no_match','error')),
    external_id  TEXT,
    error        TEXT,
    fetched_at   INTEGER NOT NULL,
    PRIMARY KEY (entity_type, entity_id, provider)
);

ALTER TABLE credits ADD COLUMN source TEXT;          -- provider that supplied the credit
CREATE INDEX idx_credits_source ON credits(source);
