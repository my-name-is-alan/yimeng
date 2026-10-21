-- 0002: denormalized display/sort columns for canonical entities, lookup
-- indexes for the catalog API, and device-token lookup for pairing auth.

ALTER TABLE artists ADD COLUMN album_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE artists ADD COLUMN track_count INTEGER NOT NULL DEFAULT 0;

ALTER TABLE albums ADD COLUMN artist_name TEXT;          -- display album artist
ALTER TABLE albums ADD COLUMN genre TEXT;
ALTER TABLE albums ADD COLUMN track_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE albums ADD COLUMN duration_ms INTEGER NOT NULL DEFAULT 0;

ALTER TABLE tracks ADD COLUMN artist_name TEXT;          -- display track artist(s)
ALTER TABLE tracks ADD COLUMN genre TEXT;
ALTER TABLE tracks ADD COLUMN year INTEGER;

CREATE INDEX idx_albums_title ON albums(title);
CREATE INDEX idx_albums_artist_name ON albums(artist_name);
CREATE INDEX idx_albums_created ON albums(created_at);
CREATE INDEX idx_tracks_title ON tracks(title);
CREATE INDEX idx_tracks_created ON tracks(created_at);

CREATE UNIQUE INDEX idx_players_token_hash ON players(token_hash) WHERE token_hash IS NOT NULL;
