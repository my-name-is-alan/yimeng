-- 0009: ReplayGain (dB relative to the RG2 -18 LUFS reference; peaks linear).
-- rg_source: tag (REPLAYGAIN_* / R128_*) | analyzed (ffmpeg ebur128).
ALTER TABLE source_tracks ADD COLUMN rg_track_gain REAL;
ALTER TABLE source_tracks ADD COLUMN rg_track_peak REAL;
ALTER TABLE source_tracks ADD COLUMN rg_album_gain REAL;
ALTER TABLE source_tracks ADD COLUMN rg_album_peak REAL;
ALTER TABLE source_tracks ADD COLUMN rg_source TEXT;
ALTER TABLE source_tracks ADD COLUMN loudness_lufs REAL;       -- integrated loudness (analysis)
ALTER TABLE source_tracks ADD COLUMN loudness_error TEXT;      -- analysis failed; not retried until the file changes
UPDATE source_tracks SET codec=NULL;                            -- re-read tags once
