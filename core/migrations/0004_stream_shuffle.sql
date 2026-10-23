-- 0004: audio stream properties (signal path), shuffle play order,
-- per-device default stream quality.

ALTER TABLE source_tracks ADD COLUMN codec TEXT;          -- NULL = not probed yet (forces re-read)
ALTER TABLE source_tracks ADD COLUMN sample_rate INTEGER;
ALTER TABLE source_tracks ADD COLUMN bit_depth INTEGER;
ALTER TABLE source_tracks ADD COLUMN channels INTEGER;
ALTER TABLE source_tracks ADD COLUMN bitrate_kbps INTEGER;
ALTER TABLE source_tracks ADD COLUMN lossless INTEGER NOT NULL DEFAULT 0;

ALTER TABLE sessions ADD COLUMN shuffle INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN shuffle_order TEXT;       -- JSON array of queue indices (play order)
ALTER TABLE sessions ADD COLUMN shuffle_seed INTEGER;

ALTER TABLE players ADD COLUMN stream_format TEXT;        -- original | aac | mp3 | opus | flac
ALTER TABLE players ADD COLUMN stream_bitrate INTEGER;    -- kbps, lossy formats only
