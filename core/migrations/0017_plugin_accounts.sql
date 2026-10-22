-- 0017: several accounts per online plugin. Only display info lives here;
-- each account's session stays in the Vault ("plugin.<id>#<account>").
-- The default (active) account's session is also "plugin.<id>".
CREATE TABLE plugin_accounts (
  plugin_id   TEXT NOT NULL,
  account_id  TEXT NOT NULL,
  nickname    TEXT NOT NULL DEFAULT '',
  avatar      TEXT NOT NULL DEFAULT '',
  vip         INTEGER NOT NULL DEFAULT 0,
  tier        TEXT NOT NULL DEFAULT '',
  method      TEXT NOT NULL DEFAULT '',
  is_default  INTEGER NOT NULL DEFAULT 0,
  added_at    INTEGER NOT NULL,
  used_at     INTEGER NOT NULL,
  PRIMARY KEY (plugin_id, account_id)
);
