-- Pinyin / searchable text for Chinese-friendly search (E1).
ALTER TABLE artists ADD COLUMN search_text TEXT NOT NULL DEFAULT '';
ALTER TABLE albums  ADD COLUMN search_text TEXT NOT NULL DEFAULT '';
ALTER TABLE tracks  ADD COLUMN search_text TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_artists_search ON artists(search_text);
CREATE INDEX IF NOT EXISTS idx_albums_search  ON albums(search_text);
CREATE INDEX IF NOT EXISTS idx_tracks_search  ON tracks(search_text);
