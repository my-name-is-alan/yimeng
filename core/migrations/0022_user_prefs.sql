-- Per-account preferences (JSON values), e.g. source switching (D50).
CREATE TABLE user_prefs (
    owner      TEXT NOT NULL,   -- user id, or "dev:<device id>"
    key        TEXT NOT NULL,
    value      TEXT NOT NULL,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (owner, key)
);
