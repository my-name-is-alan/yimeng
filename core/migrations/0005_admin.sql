-- 0005: device roles. Admin devices may use system/admin APIs (backup,
-- restore, library roots, device roles, metadata edits). The oldest paired
-- device becomes admin; when no admin exists the next paired device is admin.
ALTER TABLE players ADD COLUMN is_admin INTEGER NOT NULL DEFAULT 0;
UPDATE players SET is_admin=1 WHERE id=(SELECT id FROM players WHERE token_hash IS NOT NULL ORDER BY created_at LIMIT 1);
