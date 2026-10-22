-- Last.fm scrobbles waiting to be (re)sent (D56). Per account; rows are
-- removed once accepted or after too many attempts. No secrets here.
CREATE TABLE lastfm_queue (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    track_id   TEXT NOT NULL,
    played_at  INTEGER NOT NULL,   -- unix ms
    attempts   INTEGER NOT NULL DEFAULT 0,
    next_at    INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_lastfm_queue_next ON lastfm_queue(next_at);
