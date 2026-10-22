-- 0018: background fill-only metadata (re-applied after every merge, only into
-- still-empty fields) and user artist images (survive rescans like user_covers).
CREATE TABLE metadata_fills (
    entity_type  TEXT NOT NULL CHECK (entity_type IN ('track','album')),
    entity_id    TEXT NOT NULL,
    field        TEXT NOT NULL,
    value        TEXT NOT NULL,
    provider     TEXT NOT NULL,
    external_id  TEXT,
    filled_at    INTEGER NOT NULL,
    PRIMARY KEY (entity_type, entity_id, field)
);
CREATE TABLE artist_images (
    artist_id   TEXT PRIMARY KEY,
    path        TEXT NOT NULL,          -- inside <cache>/covers/artists
    source      TEXT NOT NULL,          -- upload | spotify | musicbrainz | netease | qq | kugou
    updated_at  INTEGER NOT NULL
);
