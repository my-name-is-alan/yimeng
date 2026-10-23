-- 0006: embedded lyrics (USLT / LYRICS / UNSYNCEDLYRICS / ©lyr). Sidecar .lrc
-- files are read on demand so edits need no rescan. NULL codec forces re-read.
ALTER TABLE source_tracks ADD COLUMN lyrics TEXT;
UPDATE source_tracks SET codec=NULL;
