-- 0007: play history / scrobbles, per-track stats, favorites, ratings.
-- No FK to tracks: user data survives a file disappearing and reappears with it.
CREATE TABLE plays (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    track_id     TEXT NOT NULL,
    player_id    TEXT,
    played_at    INTEGER NOT NULL,      -- unix ms (start of listen)
    duration_ms  INTEGER,               -- listened duration
    completed    INTEGER NOT NULL DEFAULT 0,
    source       TEXT NOT NULL DEFAULT 'core'   -- core | scrobble | subsonic
);
CREATE INDEX idx_plays_played_at ON plays(played_at);
CREATE INDEX idx_plays_track ON plays(track_id, played_at);

CREATE TABLE track_stats (
    track_id        TEXT PRIMARY KEY,
    play_count      INTEGER NOT NULL DEFAULT 0,
    last_played_at  INTEGER
);
CREATE INDEX idx_track_stats_count ON track_stats(play_count);

CREATE TABLE favorites (
    entity_type  TEXT NOT NULL CHECK (entity_type IN ('track','album','artist')),
    entity_id    TEXT NOT NULL,
    created_at   INTEGER NOT NULL,
    PRIMARY KEY (entity_type, entity_id)
);

CREATE TABLE ratings (
    entity_type  TEXT NOT NULL CHECK (entity_type IN ('track','album','artist')),
    entity_id    TEXT NOT NULL,
    rating       INTEGER NOT NULL CHECK (rating BETWEEN 1 AND 5),
    updated_at   INTEGER NOT NULL,
    PRIMARY KEY (entity_type, entity_id)
);
