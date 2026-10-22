package online

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/vault"
)

// AccountEntry is one saved account of a plugin (no secrets).
type AccountEntry struct {
	ID            string      `json:"id"`
	Nickname      string      `json:"nickname"`
	Avatar        string      `json:"avatar,omitempty"`
	AvatarPreview string      `json:"avatar_preview,omitempty"` // D60: avatar via Core's image cache (set by the gateway)
	VIP           bool        `json:"vip"`
	Tier          string      `json:"tier,omitempty"`
	Method        LoginMethod `json:"method,omitempty"`
	Default       bool        `json:"default"`
	AddedAt       time.Time   `json:"added_at"`
}

var ErrUnknownAccount = errors.New("online: unknown account")

func accountVaultKey(id, acct string) string { return vaultKey(id) + "#" + acct }

// accountID derives a stable id from the platform user id.
func accountID(acc Account) string {
	if s := strings.TrimSpace(acc.UserID); s != "" && len(s) <= 64 && !strings.ContainsAny(s, "#/ ") {
		return s
	}
	src := acc.UserID + "\x00" + acc.Nickname
	if strings.TrimSpace(src) == "\x00" {
		return "default"
	}
	h := sha256.Sum256([]byte(src))
	return "h" + hex.EncodeToString(h[:6])
}

func sessionOnly(m map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		if !persistent(k) {
			out[k] = v
		}
	}
	return out
}

// saveAccount records the active session as a saved account and makes it the default.
func (r *Registry) saveAccount(ctx context.Context, in *instance, acc Account, m LoginMethod) {
	id := in.manifest.ID
	acct := accountID(acc)
	now := time.Now().UnixMilli()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		r.log.Error("plugin account save", "plugin", id, "err", err)
		return
	}
	defer tx.Rollback()
	tx.ExecContext(ctx, `UPDATE plugin_accounts SET is_default=0 WHERE plugin_id=?`, id)
	_, err = tx.ExecContext(ctx, `INSERT INTO plugin_accounts(plugin_id, account_id, nickname, avatar, vip, tier, method, is_default, added_at, used_at)
		VALUES (?,?,?,?,?,?,?,1,?,?) ON CONFLICT(plugin_id, account_id) DO UPDATE SET nickname=excluded.nickname, avatar=excluded.avatar,
		vip=excluded.vip, tier=excluded.tier, method=CASE WHEN excluded.method<>'' THEN excluded.method ELSE plugin_accounts.method END,
		is_default=1, used_at=excluded.used_at`, id, acct, acc.Nickname, acc.Avatar, acc.VIP, acc.Tier, string(m), now, now)
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		r.log.Error("plugin account save", "plugin", id, "err", err)
		return
	}
	r.mu.Lock()
	in.acctID = acct
	r.mu.Unlock()
	if r.vault != nil {
		if err := r.vault.Put(ctx, accountVaultKey(id, acct), vault.Credential{Method: vault.LoginMethod(m), Fields: sessionOnly(in.client.Cookies())}); err != nil {
			r.log.Error("plugin account vault", "plugin", id, "err", err)
		}
	}
}

// updateAccountInfo refreshes display fields of the active account.
func (r *Registry) updateAccountInfo(ctx context.Context, in *instance, acc Account) {
	r.mu.Lock()
	acct, method := in.acctID, in.method
	r.mu.Unlock()
	if acct == "" {
		// session from before multi-account support: register it now
		r.saveAccount(ctx, in, acc, method)
		return
	}
	r.db.ExecContext(ctx, `UPDATE plugin_accounts SET nickname=?, avatar=?, vip=?, tier=? WHERE plugin_id=? AND account_id=?`,
		acc.Nickname, acc.Avatar, acc.VIP, acc.Tier, in.manifest.ID, acct)
}

func (r *Registry) defaultAccount(ctx context.Context, id string) string {
	var acct string
	r.db.QueryRowContext(ctx, `SELECT account_id FROM plugin_accounts WHERE plugin_id=? AND is_default=1`, id).Scan(&acct)
	return acct
}

// Accounts lists the saved accounts of a plugin, default first.
func (r *Registry) Accounts(ctx context.Context, id string) ([]AccountEntry, error) {
	if _, err := r.get(id); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT account_id, nickname, avatar, vip, tier, method, is_default, added_at
		FROM plugin_accounts WHERE plugin_id=? ORDER BY is_default DESC, used_at DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AccountEntry{}
	for rows.Next() {
		var e AccountEntry
		var m string
		var added int64
		if err := rows.Scan(&e.ID, &e.Nickname, &e.Avatar, &e.VIP, &e.Tier, &m, &e.Default, &added); err != nil {
			return nil, err
		}
		e.Method, e.AddedAt = LoginMethod(m), time.UnixMilli(added).UTC()
		out = append(out, e)
	}
	return out, rows.Err()
}

// SwitchAccount makes a saved account the default: its Vault session is
// loaded into the plugin's jar (device/secret keys are kept) and becomes
// "plugin.<id>". Playback and catalog calls use the default account.
func (r *Registry) SwitchAccount(ctx context.Context, id, acct string) (Status, error) {
	in, err := r.get(id)
	if err != nil {
		return Status{}, err
	}
	if r.vault == nil {
		return Status{}, ErrNotSupported
	}
	var method string
	if err := r.db.QueryRowContext(ctx, `SELECT method FROM plugin_accounts WHERE plugin_id=? AND account_id=?`, id, acct).Scan(&method); err != nil {
		return Status{}, ErrUnknownAccount
	}
	c, err := r.vault.Get(ctx, accountVaultKey(id, acct))
	if err != nil {
		return Status{}, fmt.Errorf("%w: session missing, log in again", ErrUnknownAccount)
	}
	jar := deviceFields(in.client.Cookies())
	for k, v := range sessionOnly(c.Fields) {
		jar[k] = v
	}
	in.client.ReplaceCookies(jar)
	in.client.TakeDirty()
	r.mu.Lock()
	cfg := in.config
	r.mu.Unlock()
	p, err := in.factory(Env{HTTP: in.client, Config: cfg})
	if err != nil {
		return Status{}, err
	}
	now := time.Now().UnixMilli()
	if _, err := r.db.ExecContext(ctx, `UPDATE plugin_accounts SET is_default=(account_id=?), used_at=CASE WHEN account_id=? THEN ? ELSE used_at END WHERE plugin_id=?`, acct, acct, now, id); err != nil {
		return Status{}, err
	}
	r.mu.Lock()
	in.plugin, in.loggedIn, in.method, in.account, in.acctID, in.lastErr = p, true, LoginMethod(method), nil, acct, ""
	r.mu.Unlock()
	if err := r.vault.Put(ctx, vaultKey(id), vault.Credential{Method: vault.LoginMethod(method), Fields: jar}); err != nil {
		return Status{}, err
	}
	r.checkAccount(ctx, in, true)
	return r.status(in), nil
}

// RemoveAccount forgets a saved account (Vault session and list entry).
// Removing the default account signs it out and switches to the most
// recently used remaining account, if any.
func (r *Registry) RemoveAccount(ctx context.Context, id, acct string) (Status, error) {
	in, err := r.get(id)
	if err != nil {
		return Status{}, err
	}
	res, err := r.db.ExecContext(ctx, `DELETE FROM plugin_accounts WHERE plugin_id=? AND account_id=?`, id, acct)
	if err != nil {
		return Status{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Status{}, ErrUnknownAccount
	}
	if r.vault != nil {
		if err := r.vault.Delete(ctx, accountVaultKey(id, acct)); err != nil && !errors.Is(err, vault.ErrNotFound) {
			return Status{}, err
		}
	}
	r.mu.Lock()
	active := in.acctID == acct
	r.mu.Unlock()
	if !active {
		return r.status(in), nil
	}
	if err := r.signOut(ctx, in); err != nil {
		return Status{}, err
	}
	var next string
	err = r.db.QueryRowContext(ctx, `SELECT account_id FROM plugin_accounts WHERE plugin_id=? ORDER BY used_at DESC LIMIT 1`, id).Scan(&next)
	if errors.Is(err, sql.ErrNoRows) || next == "" {
		return r.status(in), nil
	}
	return r.SwitchAccount(ctx, id, next)
}
