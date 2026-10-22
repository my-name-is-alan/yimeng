-- 0016: web console accounts (username + password). Passwords are stored as
-- argon2id PHC strings. A web login creates a players row (device token)
-- linked to the user; the user's role decides players.is_admin.
CREATE TABLE users (
  id          TEXT PRIMARY KEY,
  username    TEXT NOT NULL UNIQUE COLLATE NOCASE,
  pw_hash     TEXT NOT NULL,
  role        TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('admin','user')),
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL
);
ALTER TABLE players ADD COLUMN user_id TEXT REFERENCES users(id) ON DELETE CASCADE;
CREATE INDEX players_user ON players(user_id);
