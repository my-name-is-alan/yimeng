-- Origin of catalog entities. 'local' = built from files under a library root
-- (editable/matchable). Future online StreamSources (netease, qq, apple, ...)
-- must insert their items with their own origin; those are read-only for
-- metadata editing, matching, cover replace and lyrics binding.
ALTER TABLE tracks  ADD COLUMN origin TEXT NOT NULL DEFAULT 'local';
ALTER TABLE albums  ADD COLUMN origin TEXT NOT NULL DEFAULT 'local';
ALTER TABLE artists ADD COLUMN origin TEXT NOT NULL DEFAULT 'local';
