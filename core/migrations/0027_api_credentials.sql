-- Developer credentials (D59): personal API keys (yk_…, scoped bearer tokens)
-- and Subsonic app passwords. Only hashes are stored here; app passwords are
-- additionally kept encrypted in the Vault (entry "apppw:<id>") because
-- Subsonic token auth (t = md5(password + salt)) needs the plaintext.
CREATE TABLE api_credentials (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind         TEXT NOT NULL CHECK (kind IN ('api_key', 'app_password')),
    name         TEXT NOT NULL,
    scopes       TEXT NOT NULL,          -- space separated: read playback library-write admin
    prefix       TEXT NOT NULL,          -- first characters, for display
    token_hash   TEXT NOT NULL UNIQUE,   -- sha256 hex of the secret
    allow_plain  INTEGER NOT NULL DEFAULT 0, -- app passwords: accept Subsonic p= (plaintext)
    created_at   INTEGER NOT NULL,
    last_used_at INTEGER,
    expires_at   INTEGER,
    revoked_at   INTEGER
);
CREATE INDEX idx_api_credentials_user ON api_credentials(user_id, kind);
