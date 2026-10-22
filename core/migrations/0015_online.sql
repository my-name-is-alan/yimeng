-- 0015: online source plugins.
-- Plugin settings (enable flag + non-secret config JSON). Credentials live in
-- the Vault (vault_entries), never here.
CREATE TABLE plugin_settings (
    id          TEXT PRIMARY KEY,
    enabled     INTEGER NOT NULL DEFAULT 0,
    config      TEXT NOT NULL DEFAULT '{}',
    updated_at  INTEGER NOT NULL
);
-- External catalog cache: raw normalized DTO JSON keyed "origin:kind:id".
CREATE TABLE external_cache (
    key         TEXT PRIMARY KEY,
    body        TEXT NOT NULL,
    fetched_at  INTEGER NOT NULL,
    expires_at  INTEGER NOT NULL
);
CREATE INDEX idx_external_cache_exp ON external_cache(expires_at);
-- Equivalent recordings across origins (external <-> local, external <-> external).
CREATE TABLE track_equivalents (
    track_id     TEXT NOT NULL,
    alt_track_id TEXT NOT NULL,
    confidence   REAL NOT NULL,
    method       TEXT NOT NULL,          -- isrc | meta
    created_at   INTEGER NOT NULL,
    PRIMARY KEY (track_id, alt_track_id)
);
CREATE INDEX idx_track_equivalents_alt ON track_equivalents(alt_track_id);
CREATE INDEX idx_tracks_origin ON tracks(origin);
CREATE INDEX idx_albums_origin ON albums(origin);
