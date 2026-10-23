package online

import (
	"context"
	"errors"
	"fmt"
	"github.com/my-name-is-alan/yimeng/core/internal/vault"
	"time"
)

// MigrateLegacy is idempotent: credentials are copied and verified before the
// ownership marker is committed. Ambiguous multi-admin installs require claim.
// A durable cleanup queue removes old credentials only after ownership commits.
func (a *Accounts) MigrateLegacy(ctx context.Context, claim string) error {
	a.migration.Lock()
	defer a.migration.Unlock()
	if a.migrationDone {
		return nil
	}
	var done int
	if err := a.db.QueryRowContext(ctx, `SELECT count(*) FROM online_legacy_migration`).Scan(&done); err != nil {
		return err
	}
	if done != 0 {
		return a.cleanupLegacy(ctx)
	}
	uid := claim
	if uid != "" {
		role, err := a.role(ctx, uid)
		if err != nil || role != "admin" {
			return ErrIdentityRequired
		}
	} else {
		var count int
		if err := a.db.QueryRowContext(ctx, `SELECT count(*),COALESCE(min(id),'') FROM users WHERE role='admin'`).Scan(&count, &uid); err != nil {
			return err
		}
		if count != 1 {
			return nil
		}
	}
	if _, err := a.db.ExecContext(ctx, `INSERT INTO online_owner_state(user_id,namespace) VALUES(?,?) ON CONFLICT(user_id) DO NOTHING`, uid, namespace(uid)); err != nil {
		return err
	}
	v := personalVault{a, uid}
	var copiedKeys []string
	legacyDefaults := map[string]vault.Credential{}
	for _, id := range a.base.Reg.IDs() {
		in, err := a.base.Reg.get(id)
		if err != nil {
			return err
		}
		if len(in.manifest.Capabilities) == 0 {
			continue
		}
		keys := []string{vaultKey(id)}
		rows, err := a.db.QueryContext(ctx, `SELECT account_id FROM plugin_accounts WHERE plugin_id=?`, id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var acct string
			if err := rows.Scan(&acct); err != nil {
				rows.Close()
				return err
			}
			keys = append(keys, accountVaultKey(id, acct))
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, key := range keys {
			cred, err := a.base.Reg.vault.Get(ctx, key)
			if errors.Is(err, vault.ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			if key == vaultKey(id) && len(keys) == 1 && hasSession(cred.Fields) {
				legacyDefaults[id] = cred
				if err := v.Put(ctx, accountVaultKey(id, "legacy"), vault.Credential{Method: cred.Method, Fields: sessionOnly(cred.Fields)}); err != nil {
					return err
				}
				if _, err := v.Get(ctx, accountVaultKey(id, "legacy")); err != nil {
					return err
				}
			}
			copiedKeys = append(copiedKeys, key)
			if err := v.Put(ctx, key, cred); err != nil {
				return err
			}
			if _, err := v.Get(ctx, key); err != nil {
				return fmt.Errorf("verify migrated session: %w", err)
			}
		}
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO plugin_accounts(plugin_id,account_id,nickname,avatar,vip,tier,method,is_default,added_at,used_at)
		SELECT ?||'.'||plugin_id,account_id,nickname,avatar,vip,tier,method,is_default,added_at,used_at FROM plugin_accounts WHERE instr(plugin_id,'.')=0`, namespace(uid))
	if err != nil {
		return err
	}
	for id, cred := range legacyDefaults {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO plugin_accounts(plugin_id,account_id,nickname,method,is_default,added_at,used_at) VALUES(?,'legacy','迁移账号',?,1,?,?)`, namespace(uid)+"."+id, string(cred.Method), time.Now().UnixMilli(), time.Now().UnixMilli()); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO online_legacy_migration VALUES(1,?,?)`, uid, time.Now().UnixMilli()); err != nil {
		return err
	}
	for _, key := range copiedKeys {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO online_legacy_cleanup(source) VALUES(?)`, key); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM plugin_accounts WHERE instr(plugin_id,'.')=0`); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return a.cleanupLegacy(ctx)
}

func (a *Accounts) cleanupLegacy(ctx context.Context) error {
	rows, err := a.db.QueryContext(ctx, `SELECT source FROM online_legacy_cleanup`)
	if err != nil {
		return err
	}
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			return err
		}
		keys = append(keys, key)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, key := range keys {
		if err := a.base.Reg.vault.Delete(ctx, key); err != nil && !errors.Is(err, vault.ErrNotFound) {
			return err
		}
		if _, err := a.db.ExecContext(ctx, `DELETE FROM online_legacy_cleanup WHERE source=?`, key); err != nil {
			return err
		}
	}
	a.base.Reg.mu.Lock()
	for _, in := range a.base.Reg.inst {
		if len(in.manifest.Capabilities) == 0 {
			continue
		}
		in.client.ReplaceCookies(deviceFields(in.client.Cookies()))
		in.client.TakeDirty()
		in.loggedIn = false
		in.account = nil
		in.acctID = ""
		in.method = ""
	}
	a.base.Reg.mu.Unlock()
	a.migrationDone = true
	return nil
}

func (a *Accounts) LegacyPending(ctx context.Context) bool {
	var n int
	err := a.db.QueryRowContext(ctx, `SELECT count(*) FROM online_legacy_migration`).Scan(&n)
	return err == nil && n == 0
}
