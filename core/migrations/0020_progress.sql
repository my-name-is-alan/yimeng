-- Resume points per user (device for code-paired devices), synced across apps.
CREATE TABLE playback_progress (
    owner       TEXT NOT NULL,     -- user id, or "dev:<device id>"
    track_id    TEXT NOT NULL,
    position_ms INTEGER NOT NULL,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    finished    INTEGER NOT NULL DEFAULT 0,
    device_id   TEXT NOT NULL DEFAULT '',
    updated_at  INTEGER NOT NULL,
    PRIMARY KEY (owner, track_id)
);
CREATE INDEX idx_progress_recent ON playback_progress(owner, updated_at DESC);
