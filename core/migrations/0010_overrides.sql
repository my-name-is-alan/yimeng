-- 0010: user metadata overrides that survive rescans (applied after Merge).
CREATE TABLE metadata_overrides (
    entity_type  TEXT NOT NULL CHECK (entity_type IN ('track','album','artist')),
    entity_id    TEXT NOT NULL,
    patch_json   TEXT NOT NULL,   -- JSON object of fields to force
    updated_at   INTEGER NOT NULL,
    PRIMARY KEY (entity_type, entity_id)
);
