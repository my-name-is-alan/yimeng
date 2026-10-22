-- 0021: per-account ownership (D49). Playlists, favorites, ratings, play
-- history and progress belong to the account that owns the device. Existing
-- rows and account-less devices go to the first admin account ("" when the
-- install has no accounts yet). Platform logins and caches stay server-wide.

CREATE TEMP TABLE _owner AS
    SELECT COALESCE((SELECT id FROM users WHERE role='admin' ORDER BY created_at, id LIMIT 1), '') AS id;

UPDATE players SET user_id = (SELECT NULLIF(id,'') FROM _owner) WHERE user_id IS NULL;

ALTER TABLE playlists ADD COLUMN owner_id TEXT NOT NULL DEFAULT '';
UPDATE playlists SET owner_id = (SELECT id FROM _owner);
CREATE INDEX idx_playlists_owner ON playlists(owner_id, name);

ALTER TABLE plays ADD COLUMN user_id TEXT NOT NULL DEFAULT '';
UPDATE plays SET user_id = COALESCE((SELECT p.user_id FROM players p WHERE p.id = plays.player_id), (SELECT id FROM _owner), '');
CREATE INDEX idx_plays_user ON plays(user_id, played_at);

CREATE TABLE favorites_new (
    user_id      TEXT NOT NULL DEFAULT '',
    entity_type  TEXT NOT NULL CHECK (entity_type IN ('track','album','artist')),
    entity_id    TEXT NOT NULL,
    created_at   INTEGER NOT NULL,
    PRIMARY KEY (user_id, entity_type, entity_id)
);
INSERT INTO favorites_new(user_id, entity_type, entity_id, created_at)
    SELECT (SELECT id FROM _owner), entity_type, entity_id, created_at FROM favorites;
DROP TABLE favorites;
ALTER TABLE favorites_new RENAME TO favorites;

CREATE TABLE ratings_new (
    user_id      TEXT NOT NULL DEFAULT '',
    entity_type  TEXT NOT NULL CHECK (entity_type IN ('track','album','artist')),
    entity_id    TEXT NOT NULL,
    rating       INTEGER NOT NULL CHECK (rating BETWEEN 1 AND 5),
    updated_at   INTEGER NOT NULL,
    PRIMARY KEY (user_id, entity_type, entity_id)
);
INSERT INTO ratings_new(user_id, entity_type, entity_id, rating, updated_at)
    SELECT (SELECT id FROM _owner), entity_type, entity_id, rating, updated_at FROM ratings;
DROP TABLE ratings;
ALTER TABLE ratings_new RENAME TO ratings;

-- progress rows keyed "dev:<id>" move to the device's account
UPDATE OR REPLACE playback_progress SET owner = COALESCE((SELECT p.user_id FROM players p WHERE 'dev:' || p.id = playback_progress.owner), owner)
    WHERE owner LIKE 'dev:%';

DROP TABLE _owner;
