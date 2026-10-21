-- 0008: playlists. Static playlists keep ordered items (no FK to tracks so a
-- temporarily missing file keeps its slot); smart playlists store rule JSON
-- evaluated at read time.
CREATE TABLE playlists (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    comment     TEXT,
    rules       TEXT,                 -- smart playlist rule JSON; NULL = static
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);
CREATE TABLE playlist_items (
    playlist_id  TEXT NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
    pos          INTEGER NOT NULL,
    track_id     TEXT NOT NULL,
    PRIMARY KEY (playlist_id, pos)
);
CREATE INDEX idx_playlist_items_track ON playlist_items(track_id);
