-- Classical / multi-disc helpers + 100k-track indexes (E3, E9).
ALTER TABLE tracks ADD COLUMN composer TEXT;
ALTER TABLE albums ADD COLUMN disc_count INTEGER NOT NULL DEFAULT 1;

CREATE INDEX IF NOT EXISTS idx_tracks_composer ON tracks(composer) WHERE composer IS NOT NULL AND composer <> '';
CREATE INDEX IF NOT EXISTS idx_tracks_disc ON tracks(album_id, disc_no, track_no);
CREATE INDEX IF NOT EXISTS idx_tracks_search_text ON tracks(search_text);
CREATE INDEX IF NOT EXISTS idx_albums_search_text ON albums(search_text);
CREATE INDEX IF NOT EXISTS idx_artists_search_text ON artists(search_text);
CREATE INDEX IF NOT EXISTS idx_tracks_genre ON tracks(genre);
