-- Platform cache: stale-while-revalidate horizon, negative entries, size and
-- last access for LRU eviction.
ALTER TABLE external_cache ADD COLUMN stale_until INTEGER NOT NULL DEFAULT 0;
ALTER TABLE external_cache ADD COLUMN negative TEXT NOT NULL DEFAULT '';
ALTER TABLE external_cache ADD COLUMN size INTEGER NOT NULL DEFAULT 0;
ALTER TABLE external_cache ADD COLUMN last_access INTEGER NOT NULL DEFAULT 0;
UPDATE external_cache SET stale_until = expires_at, size = length(body), last_access = fetched_at;
CREATE INDEX idx_external_cache_stale ON external_cache(stale_until);
CREATE INDEX idx_external_cache_access ON external_cache(last_access);
