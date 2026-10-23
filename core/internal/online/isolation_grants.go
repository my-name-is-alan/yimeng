package online

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/my-name-is-alan/yimeng/core/internal/owner"
	"strings"
	"time"
	"unicode/utf8"
)

type Grant struct {
	Available         bool   `json:"available"`
	UnavailableReason string `json:"unavailable_reason,omitempty"`
	ID                string `json:"id"`
	ProviderUserID    string `json:"provider_user_id,omitempty"`
	ConsumerUserID    string `json:"consumer_user_id"`
	Platform          string `json:"platform"`
	AccountID         string `json:"account_id,omitempty"`
	Label             string `json:"label"`
	Revision          int64  `json:"revision"`
	Revoked           bool   `json:"revoked"`
}

func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

func (a *Accounts) Grants(ctx context.Context) ([]Grant, error) {
	uid := owner.From(ctx).ID
	role, err := a.role(ctx, uid)
	if err != nil {
		return nil, err
	}
	where := "consumer_user_id=?"
	if role == "admin" {
		where = "provider_user_id=?"
	}
	rows, err := a.db.QueryContext(ctx, `SELECT id,provider_user_id,consumer_user_id,platform,account_id,label,revision,revoked FROM stream_grants WHERE `+where+` ORDER BY platform,consumer_user_id`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Grant{}
	for rows.Next() {
		var g Grant
		if err := rows.Scan(&g.ID, &g.ProviderUserID, &g.ConsumerUserID, &g.Platform, &g.AccountID, &g.Label, &g.Revision, &g.Revoked); err != nil {
			return nil, err
		}
		if _, err := a.permit(owner.With(ctx, g.ConsumerUserID, false), g.Platform); err == nil {
			g.Available = true
		} else {
			switch {
			case errors.Is(err, ErrGrantRevoked):
				g.UnavailableReason = "shared_stream_revoked"
			case errors.Is(err, ErrSharedNeedsLogin):
				g.UnavailableReason = "shared_account_needs_login"
			case errors.Is(err, ErrDisabled):
				g.UnavailableReason = "plugin_disabled"
			default:
				g.UnavailableReason = "unavailable"
			}
		}
		if role != "admin" {
			g.ProviderUserID = ""
			g.AccountID = ""
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (a *Accounts) Grant(ctx context.Context, consumer, platform, account, label string) (Grant, error) {
	uid := owner.From(ctx).ID
	role, err := a.role(ctx, uid)
	if err != nil || role != "admin" {
		return Grant{}, ErrIdentityRequired
	}
	cr, err := a.role(ctx, consumer)
	if err != nil || cr != "user" {
		return Grant{}, fmt.Errorf("%w: recipient must be a member account", ErrBadInput)
	}
	label = strings.TrimSpace(label)
	if label == "" {
		label = "管理员共享"
	}
	if utf8.RuneCountInString(label) > 80 {
		return Grant{}, ErrBadInput
	}
	var n int
	if err := a.db.QueryRowContext(ctx, `SELECT count(*) FROM plugin_accounts WHERE plugin_id=? AND account_id=?`, namespace(uid)+"."+platform, account).Scan(&n); err != nil {
		return Grant{}, err
	}
	if n != 1 {
		return Grant{}, ErrUnknownAccount
	}
	cred, err := (personalVault{a, uid}).Get(ctx, accountVaultKey(platform, account))
	if err != nil || !hasSession(cred.Fields) {
		return Grant{}, ErrNeedsLogin
	}
	p, _, err := a.base.Reg.Enabled(platform)
	if err != nil {
		return Grant{}, err
	}
	if !p.Manifest().Has(CapStream) {
		return Grant{}, ErrNotSupported
	}
	a.activeMu.Lock()
	defer a.activeMu.Unlock()
	g := Grant{}
	err = a.db.QueryRowContext(ctx, `INSERT INTO stream_grants(id,provider_user_id,consumer_user_id,platform,account_id,label,created_at)
		VALUES(?,?,?,?,?,?,?) ON CONFLICT(consumer_user_id,platform) DO UPDATE SET account_id=excluded.account_id,label=excluded.label,
		revision=stream_grants.revision+1,revoked=0 WHERE stream_grants.provider_user_id=excluded.provider_user_id
		RETURNING id,provider_user_id,consumer_user_id,platform,account_id,label,revision,revoked`, randomID(), uid, consumer, platform, account, label, time.Now().UnixMilli()).
		Scan(&g.ID, &g.ProviderUserID, &g.ConsumerUserID, &g.Platform, &g.AccountID, &g.Label, &g.Revision, &g.Revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return g, ErrGrantConflict
	}
	if err == nil {
		a.cancelGrantLocked(g.ID)
		g.Available = true
	}
	return g, err
}

func (a *Accounts) Revoke(ctx context.Context, id string) error {
	uid := owner.From(ctx).ID
	role, err := a.role(ctx, uid)
	if err != nil || role != "admin" {
		return ErrIdentityRequired
	}
	a.activeMu.Lock()
	defer a.activeMu.Unlock()
	res, err := a.db.ExecContext(ctx, `UPDATE stream_grants SET revoked=1,revision=revision+1 WHERE id=? AND provider_user_id=?`, id, uid)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrUnknownAccount
	}
	a.cancelGrantLocked(id)
	return nil
}
