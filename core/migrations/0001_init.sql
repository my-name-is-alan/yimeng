-- 0001_init: P0 schema per docs/core-architecture.md §6.
-- Timestamps are unix milliseconds (INTEGER) unless noted.

-- ── canonical entities (UI reads only this layer) ──────────────────────────
CREATE TABLE artists (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    mb_id       TEXT,
    sort_name   TEXT,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);
CREATE INDEX idx_artists_mb_id ON artists(mb_id);
CREATE INDEX idx_artists_name ON artists(name);

CREATE TABLE albums (
    id             TEXT PRIMARY KEY,
    title          TEXT NOT NULL,
    year           INTEGER,
    cover_path     TEXT,
    mb_release_id  TEXT,
    created_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL
);
CREATE INDEX idx_albums_mb_release_id ON albums(mb_release_id);

CREATE TABLE tracks (
    id               TEXT PRIMARY KEY,
    title            TEXT NOT NULL,
    album_id         TEXT REFERENCES albums(id) ON DELETE SET NULL,
    disc_no          INTEGER,
    track_no         INTEGER,
    duration_ms      INTEGER,
    isrc             TEXT,
    mb_recording_id  TEXT,
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL
);
CREATE INDEX idx_tracks_album_id ON tracks(album_id);
CREATE INDEX idx_tracks_isrc ON tracks(isrc);
CREATE INDEX idx_tracks_mb_recording_id ON tracks(mb_recording_id);

CREATE TABLE track_artists (
    track_id   TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    artist_id  TEXT NOT NULL REFERENCES artists(id) ON DELETE CASCADE,
    role       TEXT NOT NULL DEFAULT 'primary',   -- primary | feat | ...
    position   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (track_id, artist_id, role)
);
CREATE INDEX idx_track_artists_artist ON track_artists(artist_id);

CREATE TABLE album_artists (
    album_id   TEXT NOT NULL REFERENCES albums(id) ON DELETE CASCADE,
    artist_id  TEXT NOT NULL REFERENCES artists(id) ON DELETE CASCADE,
    role       TEXT NOT NULL DEFAULT 'primary',
    position   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (album_id, artist_id, role)
);
CREATE INDEX idx_album_artists_artist ON album_artists(artist_id);

CREATE TABLE credits (
    id        TEXT PRIMARY KEY,
    track_id  TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    role      TEXT NOT NULL,
    name      TEXT NOT NULL,
    mb_id     TEXT
);
CREATE INDEX idx_credits_track ON credits(track_id);

-- ── source layer (raw per-adapter records; may be dirty / duplicated) ──────
CREATE TABLE source_tracks (
    id               TEXT PRIMARY KEY,
    source           TEXT NOT NULL,           -- local | musicbrainz | apple | ...
    source_track_id  TEXT NOT NULL,           -- local: absolute file path
    raw_json         TEXT,
    path             TEXT,
    root             TEXT,                    -- library root the file was found under
    size             INTEGER,
    mtime            INTEGER,                 -- unix nanoseconds
    hash             TEXT,                    -- quick fingerprint: sha1(size + first 64 KiB)
    format           TEXT,
    title            TEXT,
    artist           TEXT,
    album            TEXT,
    album_artist     TEXT,
    track_no         INTEGER,
    track_total      INTEGER,
    disc_no          INTEGER,
    disc_total       INTEGER,
    year             INTEGER,
    genre            TEXT,
    duration_ms      INTEGER,
    isrc             TEXT,
    mb_recording_id  TEXT,
    mb_release_id    TEXT,
    cover_path       TEXT,
    cover_source     TEXT,                    -- embedded | folder | NULL
    last_scan_id     TEXT,
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL,
    UNIQUE (source, source_track_id)
);
CREATE INDEX idx_source_tracks_root_scan ON source_tracks(source, root, last_scan_id);
CREATE INDEX idx_source_tracks_album ON source_tracks(album_artist, album);

CREATE TABLE track_links (
    track_id         TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    source_track_id  TEXT NOT NULL REFERENCES source_tracks(id) ON DELETE CASCADE,
    confidence       REAL NOT NULL DEFAULT 1.0,
    PRIMARY KEY (track_id, source_track_id)
);
CREATE INDEX idx_track_links_source ON track_links(source_track_id);

-- ── playback ───────────────────────────────────────────────────────────────
CREATE TABLE players (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    device_type   TEXT,
    last_seen_at  INTEGER,
    token_hash    TEXT,
    created_at    INTEGER NOT NULL
);

CREATE TABLE sessions (
    player_id    TEXT PRIMARY KEY REFERENCES players(id) ON DELETE CASCADE,
    state        TEXT NOT NULL DEFAULT 'idle'
                 CHECK (state IN ('idle','buffering','playing','paused')),
    position_ms  INTEGER NOT NULL DEFAULT 0,
    updated_at   INTEGER NOT NULL
);

CREATE TABLE queue_items (
    player_id  TEXT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    idx        INTEGER NOT NULL,
    track_id   TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    UNIQUE (player_id, idx)
);

-- ── credentials & settings ────────────────────────────────────────────────
CREATE TABLE vault_entries (
    source      TEXT PRIMARY KEY,
    blob        BLOB NOT NULL,                -- encrypted; never plaintext
    updated_at  INTEGER NOT NULL
);

CREATE TABLE settings (
    key    TEXT PRIMARY KEY,
    value  TEXT
);

CREATE TABLE scan_jobs (
    id           TEXT PRIMARY KEY,
    root         TEXT NOT NULL,               -- JSON array of roots covered by this job
    status       TEXT NOT NULL CHECK (status IN ('queued','running','completed','failed')),
    progress     REAL NOT NULL DEFAULT 0,     -- 0..1
    total        INTEGER NOT NULL DEFAULT 0,
    processed    INTEGER NOT NULL DEFAULT 0,
    added        INTEGER NOT NULL DEFAULT 0,
    updated      INTEGER NOT NULL DEFAULT 0,
    unchanged    INTEGER NOT NULL DEFAULT 0,
    removed      INTEGER NOT NULL DEFAULT 0,
    failed       INTEGER NOT NULL DEFAULT 0,
    error        TEXT,
    started_at   INTEGER,
    finished_at  INTEGER
);
CREATE INDEX idx_scan_jobs_started ON scan_jobs(started_at);
