// Package auth implements LAN pairing (docs §4.1): a short-lived single-use
// pairing code is exchanged for a long-lived device token. Only SHA-256
// hashes of codes and tokens are kept (codes in memory, tokens in
// players.token_hash); comparisons are constant-time; pairing attempts are
// rate-limited per client IP and globally.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidCode  = errors.New("invalid or expired pairing code")
	ErrRateLimited  = errors.New("too many pairing attempts")
	ErrUnauthorized = errors.New("unauthorized")
	ErrNotFound     = errors.New("device not found")
	ErrTooManyCodes = errors.New("too many active pairing codes")
)

const (
	codeAlphabet   = "ABCDEFGHJKMNPQRSTUVWXYZ23456789" // no 0/O/1/I/L
	codeLen        = 8                                 // ~39.6 bits
	tokenPrefix    = "ymd_"
	maxActiveCodes = 5

	ipFailLimit     = 5  // failed attempts per IP per window
	globalFailLimit = 30 // failed attempts across all IPs per window
	failWindow      = time.Minute
	requestLimit    = 3 // unauthenticated code requests per IP per window
)

type Device struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	DeviceType string    `json:"device_type,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at,omitzero"`
}

type codeEntry struct {
	hash    [32]byte
	expires time.Time
}

type window struct {
	start time.Time
	n     int
}

type Service struct {
	db  *sql.DB
	ttl time.Duration
	now func() time.Time

	mu       sync.Mutex
	codes    []codeEntry
	fails    map[string]*window
	requests map[string]*window
	global   window
}

func New(db *sql.DB, codeTTL time.Duration) *Service {
	return &Service{db: db, ttl: codeTTL, now: time.Now, fails: map[string]*window{}, requests: map[string]*window{}}
}

func hashCode(code string) [32]byte {
	c := strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(code)))
	return sha256.Sum256([]byte("pair:" + c))
}

func HashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

func randomCode() string {
	b := make([]byte, codeLen)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	out := make([]byte, 0, codeLen+1)
	for i, x := range b {
		if i == codeLen/2 {
			out = append(out, '-')
		}
		// 256 % 31 bias is negligible against the rate limit; use rejection anyway.
		for int(x) >= 256-256%len(codeAlphabet) {
			var r [1]byte
			rand.Read(r[:])
			x = r[0]
		}
		out = append(out, codeAlphabet[int(x)%len(codeAlphabet)])
	}
	return string(out)
}

func (s *Service) pruneLocked(now time.Time) {
	keep := s.codes[:0]
	for _, c := range s.codes {
		if now.Before(c.expires) {
			keep = append(keep, c)
		}
	}
	s.codes = keep
	for k, w := range s.fails {
		if now.Sub(w.start) > failWindow {
			delete(s.fails, k)
		}
	}
	for k, w := range s.requests {
		if now.Sub(w.start) > failWindow {
			delete(s.requests, k)
		}
	}
}

func bump(m map[string]*window, key string, now time.Time) int {
	w := m[key]
	if w == nil || now.Sub(w.start) > failWindow {
		w = &window{start: now}
		m[key] = w
	}
	w.n++
	return w.n
}

// NewCode issues a pairing code. The plaintext is returned once and never stored.
func (s *Service) NewCode() (string, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.pruneLocked(now)
	if len(s.codes) >= maxActiveCodes {
		return "", time.Time{}, ErrTooManyCodes
	}
	code := randomCode()
	exp := now.Add(s.ttl)
	s.codes = append(s.codes, codeEntry{hash: hashCode(code), expires: exp})
	return code, exp, nil
}

// RequestCode is the unauthenticated bootstrap path: the caller never sees the
// code; the server operator reads it from the Core log. Rate-limited per IP.
func (s *Service) RequestCode(ip string) (string, time.Time, error) {
	s.mu.Lock()
	n := bump(s.requests, ip, s.now())
	s.mu.Unlock()
	if n > requestLimit {
		return "", time.Time{}, ErrRateLimited
	}
	return s.NewCode()
}

// Pair redeems a code for a new device and returns its token (shown once).
func (s *Service) Pair(ctx context.Context, ip, code, name, deviceType string) (Device, string, error) {
	s.mu.Lock()
	now := s.now()
	s.pruneLocked(now)
	if w := s.fails[ip]; (w != nil && w.n >= ipFailLimit) ||
		(now.Sub(s.global.start) <= failWindow && s.global.n >= globalFailLimit) {
		s.mu.Unlock()
		return Device{}, "", ErrRateLimited
	}
	h := hashCode(code)
	idx := -1
	for i, c := range s.codes {
		// constant-time over every active code; no early exit
		if subtle.ConstantTimeCompare(h[:], c.hash[:]) == 1 {
			idx = i
		}
	}
	if idx < 0 {
		bump(s.fails, ip, now)
		if now.Sub(s.global.start) > failWindow {
			s.global = window{start: now}
		}
		s.global.n++
		s.mu.Unlock()
		return Device{}, "", ErrInvalidCode
	}
	s.codes = append(s.codes[:idx], s.codes[idx+1:]...) // single use
	delete(s.fails, ip)
	s.mu.Unlock()

	name = strings.TrimSpace(name)
	if name == "" {
		name = "Unnamed device"
	}
	if len(name) > 100 {
		name = name[:100]
	}
	if len(deviceType) > 32 {
		deviceType = deviceType[:32]
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return Device{}, "", err
	}
	token := tokenPrefix + base64.RawURLEncoding.EncodeToString(raw[:])
	var idb [16]byte
	rand.Read(idb[:])
	d := Device{ID: "dev_" + hex.EncodeToString(idb[:]), Name: name, DeviceType: deviceType, CreatedAt: now.UTC().Truncate(time.Millisecond)}
	_, err := s.db.ExecContext(ctx, `INSERT INTO players(id, name, device_type, token_hash, created_at, last_seen_at) VALUES (?,?,?,?,?,?)`,
		d.ID, d.Name, nullStr(deviceType), HashToken(token), now.UnixMilli(), now.UnixMilli())
	if err != nil {
		return Device{}, "", err
	}
	d.LastSeenAt = d.CreatedAt
	return d, token, nil
}

// Authenticate resolves a bearer token to its device.
func (s *Service) Authenticate(ctx context.Context, token string) (Device, error) {
	if !strings.HasPrefix(token, tokenPrefix) || len(token) > 128 {
		return Device{}, ErrUnauthorized
	}
	h := HashToken(token)
	var d Device
	var stored string
	var dt sql.NullString
	var created int64
	var seen sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT id, name, device_type, token_hash, created_at, last_seen_at FROM players WHERE token_hash=?`, h).
		Scan(&d.ID, &d.Name, &dt, &stored, &created, &seen)
	if err != nil || subtle.ConstantTimeCompare([]byte(stored), []byte(h)) != 1 {
		return Device{}, ErrUnauthorized
	}
	d.DeviceType = dt.String
	d.CreatedAt = time.UnixMilli(created).UTC()
	now := s.now()
	if !seen.Valid || now.Sub(time.UnixMilli(seen.Int64)) > time.Minute {
		s.db.ExecContext(ctx, `UPDATE players SET last_seen_at=? WHERE id=?`, now.UnixMilli(), d.ID)
		seen = sql.NullInt64{Int64: now.UnixMilli(), Valid: true}
	}
	d.LastSeenAt = time.UnixMilli(seen.Int64).UTC()
	return d, nil
}

func (s *Service) ListDevices(ctx context.Context) ([]Device, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, COALESCE(device_type,''), created_at, COALESCE(last_seen_at,0)
		FROM players WHERE token_hash IS NOT NULL ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Device{}
	for rows.Next() {
		var d Device
		var c, l int64
		if err := rows.Scan(&d.ID, &d.Name, &d.DeviceType, &c, &l); err != nil {
			return nil, err
		}
		d.CreatedAt = time.UnixMilli(c).UTC()
		if l > 0 {
			d.LastSeenAt = time.UnixMilli(l).UTC()
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Revoke deletes a device (its token stops working immediately).
func (s *Service) Revoke(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM players WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) DeviceCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM players WHERE token_hash IS NOT NULL`).Scan(&n)
	return n, err
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
