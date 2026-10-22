-- 0013: web-admin edits that survive rescans: bound/hand-edited lyrics and replaced album covers.
CREATE TABLE user_lyrics (
    track_id   TEXT PRIMARY KEY,
    lrc        TEXT NOT NULL,
    offset_ms  INTEGER NOT NULL DEFAULT 0, -- positive = lyrics appear sooner (LRC convention)
    source     TEXT NOT NULL DEFAULT 'manual', -- manual | lrclib | ...
    updated_at INTEGER NOT NULL
);
CREATE TABLE user_covers (
    album_id   TEXT PRIMARY KEY,
    path       TEXT NOT NULL, -- inside <cache>/covers/custom
    updated_at INTEGER NOT NULL
);
