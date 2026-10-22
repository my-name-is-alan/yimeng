-- Last known entitlement of stored online tracks (snapshots in playlists,
-- history) so unavailable/preview items can be marked (D51).
CREATE TABLE external_status (
    track_id    TEXT PRIMARY KEY REFERENCES tracks(id) ON DELETE CASCADE,
    entitlement TEXT NOT NULL,     -- full | preview | unavailable
    checked_at  INTEGER NOT NULL
);
