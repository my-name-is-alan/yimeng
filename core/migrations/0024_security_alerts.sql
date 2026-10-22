-- Security alerts (D54): new device pairing, new-IP login, repeated failed
-- logins and lockouts. Shown to admins (all) and to the affected account.
CREATE TABLE security_events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    at         INTEGER NOT NULL,
    kind       TEXT NOT NULL,      -- device_paired | new_ip_login | login_failed | lockout
    severity   TEXT NOT NULL,      -- info | warn | critical
    user_id    TEXT NOT NULL DEFAULT '',
    device_id  TEXT NOT NULL DEFAULT '',
    ip         TEXT NOT NULL DEFAULT '',
    detail     TEXT NOT NULL DEFAULT '{}',
    read_at    INTEGER
);
CREATE INDEX idx_security_events_at ON security_events(at DESC);
CREATE INDEX idx_security_events_user ON security_events(user_id, at DESC);

-- IPs an account has signed in from (for new-IP alerts).
CREATE TABLE login_ips (
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    ip         TEXT NOT NULL,
    first_seen INTEGER NOT NULL,
    last_seen  INTEGER NOT NULL,
    PRIMARY KEY (user_id, ip)
);
