// Package secalert records security alerts (D54) and publishes them as
// "security.alert" events delivered to admins and the affected account.
package secalert

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/events"
)

const (
	KindDevicePaired = "device_paired"
	KindNewIPLogin   = "new_ip_login"
	KindLoginFailed  = "login_failed"
	KindLockout      = "lockout"
)

// FailThreshold failed logins (per username+IP) within FailWindow raise one alert.
const (
	FailThreshold = 3
	FailWindow    = 15 * time.Minute
)

type Alert struct {
	ID       int64          `json:"id"`
	At       time.Time      `json:"at"`
	Kind     string         `json:"kind"`
	Severity string         `json:"severity"`
	UserID   string         `json:"user_id,omitempty"`
	DeviceID string         `json:"device_id,omitempty"`
	IP       string         `json:"ip,omitempty"`
	Detail   map[string]any `json:"detail"`
	Read     bool           `json:"read"`
}

// Event is what is published on the hub; gateways deliver it to admins
// and to sessions of UserID only.
type Event struct {
	UserID string
	Alert  Alert
}

type Store struct {
	DB  *sql.DB
	Hub *events.Hub
	Now func() time.Time

	mu    sync.Mutex
	fails map[string][]time.Time // username|ip -> recent failures
	sent  map[string]time.Time   // dedupe key -> last alert
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Record stores and publishes an alert.
func (s *Store) Record(ctx context.Context, a Alert) (Alert, error) {
	if a.At.IsZero() {
		a.At = s.now().UTC()
	}
	if a.Detail == nil {
		a.Detail = map[string]any{}
	}
	b, _ := json.Marshal(a.Detail)
	res, err := s.DB.ExecContext(ctx, `INSERT INTO security_events(at, kind, severity, user_id, device_id, ip, detail) VALUES (?,?,?,?,?,?,?)`,
		a.At.UnixMilli(), a.Kind, a.Severity, a.UserID, a.DeviceID, a.IP, string(b))
	if err != nil {
		return a, err
	}
	a.ID, _ = res.LastInsertId()
	// keep the table bounded
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM security_events WHERE id <= (SELECT id FROM security_events ORDER BY id DESC LIMIT 1 OFFSET 5000)`)
	if s.Hub != nil {
		s.Hub.Publish("security.alert", Event{UserID: a.UserID, Alert: a})
	}
	return a, nil
}

// DevicePaired records a new paired device for userID.
func (s *Store) DevicePaired(ctx context.Context, userID, deviceID, name, deviceType, ip string) {
	_, _ = s.Record(ctx, Alert{Kind: KindDevicePaired, Severity: "info", UserID: userID, DeviceID: deviceID, IP: ip,
		Detail: map[string]any{"name": name, "device_type": deviceType}})
}

// LoginOK remembers the sign-in IP and alerts when an account that has
// signed in before uses a new IP.
func (s *Store) LoginOK(ctx context.Context, userID, username, deviceID, ip string) {
	if userID == "" || ip == "" {
		return
	}
	now := s.now().UnixMilli()
	var known, seen int
	_ = s.DB.QueryRowContext(ctx, `SELECT count(*), coalesce(sum(ip=?),0) FROM login_ips WHERE user_id=?`, ip, userID).Scan(&known, &seen)
	_, _ = s.DB.ExecContext(ctx, `INSERT INTO login_ips(user_id, ip, first_seen, last_seen) VALUES (?,?,?,?)
		ON CONFLICT(user_id, ip) DO UPDATE SET last_seen=excluded.last_seen`, userID, ip, now, now)
	if known > 0 && seen == 0 {
		_, _ = s.Record(ctx, Alert{Kind: KindNewIPLogin, Severity: "warn", UserID: userID, DeviceID: deviceID, IP: ip,
			Detail: map[string]any{"username": username}})
	}
}

// LoginFailed counts a failed sign-in; the FailThreshold-th failure within
// FailWindow (per username+IP) raises one alert. locked=true (the account or
// IP is now locked out) raises a lockout alert once per window.
func (s *Store) LoginFailed(ctx context.Context, username, ip string, locked bool) {
	username = strings.ToLower(strings.TrimSpace(username))
	if len(username) > 64 {
		username = username[:64]
	}
	now := s.now()
	key := username + "|" + ip
	s.mu.Lock()
	if s.fails == nil {
		s.fails, s.sent = map[string][]time.Time{}, map[string]time.Time{}
	}
	recent := s.fails[key][:0]
	for _, t := range s.fails[key] {
		if now.Sub(t) < FailWindow {
			recent = append(recent, t)
		}
	}
	recent = append(recent, now)
	s.fails[key] = recent
	if len(s.fails) > 10000 { // bound memory under spraying
		for k, v := range s.fails {
			if len(v) == 0 || now.Sub(v[len(v)-1]) >= FailWindow {
				delete(s.fails, k)
			}
		}
	}
	var kinds []string
	if len(recent) >= FailThreshold && now.Sub(s.sent["f|"+key]) >= FailWindow {
		s.sent["f|"+key] = now
		kinds = append(kinds, KindLoginFailed)
	}
	if locked && now.Sub(s.sent["l|"+key]) >= FailWindow {
		s.sent["l|"+key] = now
		kinds = append(kinds, KindLockout)
	}
	n := len(recent)
	s.mu.Unlock()
	if len(kinds) == 0 {
		return
	}
	var uid string
	_ = s.DB.QueryRowContext(ctx, `SELECT id FROM users WHERE username=? COLLATE NOCASE`, username).Scan(&uid)
	for _, k := range kinds {
		sev := "warn"
		if k == KindLockout {
			sev = "critical"
		}
		_, _ = s.Record(ctx, Alert{Kind: k, Severity: sev, UserID: uid, IP: ip,
			Detail: map[string]any{"username": username, "failures": n, "window_min": int(FailWindow.Minutes())}})
	}
}

// List returns alerts newest first: all (admin) or those of userID.
func (s *Store) List(ctx context.Context, userID string, all bool, limit int) ([]Alert, int, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	where, args := "", []any{}
	if !all {
		where, args = "WHERE user_id=?", []any{userID}
	}
	var unread int
	_ = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM security_events `+where+andOr(where)+`read_at IS NULL`, args...).Scan(&unread)
	rows, err := s.DB.QueryContext(ctx, `SELECT id, at, kind, severity, user_id, device_id, ip, detail, read_at IS NOT NULL
		FROM security_events `+where+` ORDER BY id DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Alert{}
	for rows.Next() {
		var a Alert
		var at int64
		var det string
		if err := rows.Scan(&a.ID, &at, &a.Kind, &a.Severity, &a.UserID, &a.DeviceID, &a.IP, &det, &a.Read); err != nil {
			return nil, 0, err
		}
		a.At = time.UnixMilli(at).UTC()
		_ = json.Unmarshal([]byte(det), &a.Detail)
		out = append(out, a)
	}
	return out, unread, rows.Err()
}

func andOr(where string) string {
	if where == "" {
		return " WHERE "
	}
	return " AND "
}

// MarkRead marks alerts read (ids empty = all visible to the caller).
func (s *Store) MarkRead(ctx context.Context, userID string, all bool, ids []int64) error {
	q := `UPDATE security_events SET read_at=? WHERE read_at IS NULL`
	args := []any{s.now().UnixMilli()}
	if !all {
		q += ` AND user_id=?`
		args = append(args, userID)
	}
	if len(ids) > 0 {
		q += ` AND id IN (?` + strings.Repeat(",?", len(ids)-1) + `)`
		for _, id := range ids {
			args = append(args, id)
		}
	}
	_, err := s.DB.ExecContext(ctx, q, args...)
	return err
}
