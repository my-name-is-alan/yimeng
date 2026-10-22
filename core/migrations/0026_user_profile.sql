-- Account profile (nickname + uploaded avatar). Avatars live in the DB so
-- they are part of backups; size is capped by the gateway (1 MiB).
ALTER TABLE users ADD COLUMN nickname TEXT NOT NULL DEFAULT '';
CREATE TABLE user_avatars (
    user_id    TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    mime       TEXT NOT NULL,
    data       BLOB NOT NULL,
    updated_at INTEGER NOT NULL
);
