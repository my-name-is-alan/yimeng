-- Personal platform sessions are namespaced; existing global sessions are
-- quarantined until the Go migrator assigns them to one administrator.
CREATE TABLE online_owner_state (
  user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  namespace TEXT NOT NULL UNIQUE,
  version INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE online_secret_refs (
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  source TEXT NOT NULL,
  vault_key TEXT NOT NULL UNIQUE,
  PRIMARY KEY(user_id, source)
);
CREATE TABLE stream_grants (
  id TEXT PRIMARY KEY,
  provider_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  consumer_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  platform TEXT NOT NULL,
  account_id TEXT NOT NULL,
  label TEXT NOT NULL,
  revision INTEGER NOT NULL DEFAULT 1,
  revoked INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  UNIQUE(consumer_user_id, platform)
);
CREATE TABLE online_legacy_migration (
  singleton INTEGER PRIMARY KEY CHECK(singleton=1),
  owner_user_id TEXT NOT NULL,
  completed_at INTEGER NOT NULL
);
CREATE TRIGGER online_owner_delete BEFORE DELETE ON users BEGIN
  DELETE FROM vault_entries WHERE source IN
    (SELECT vault_key FROM online_secret_refs WHERE user_id=OLD.id);
  DELETE FROM plugin_accounts WHERE plugin_id LIKE
    (SELECT namespace || '.%' FROM online_owner_state WHERE user_id=OLD.id);
  DELETE FROM external_cache WHERE key LIKE
    (SELECT namespace || '/%' FROM online_owner_state WHERE user_id=OLD.id);
END;

CREATE TABLE online_config_version (singleton INTEGER PRIMARY KEY CHECK(singleton=1), version INTEGER NOT NULL);
INSERT INTO online_config_version VALUES(1,1);

CREATE TABLE online_legacy_cleanup (source TEXT PRIMARY KEY);
