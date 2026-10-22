package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// App sessions (D45). A device keeps one long-lived, rotating refresh token
// (the classic ymd_ token, hashed in players.token_hash) and gets short-lived
// stateless access tokens (yma_…, HMAC over device id + expiry). Access tokens
// are checked against the device row, so revoking a device ends them at once.
// Signed URLs (?exp=&sig=&dev=) are path-scoped HMACs for <img>/<audio>.

const (
	accessPrefix = "yma_"
	// AccessTTL is the lifetime of an access token.
	AccessTTL = 15 * time.Minute
	// SignedURLTTL is the default lifetime of a signed media URL.
	SignedURLTTL = 6 * time.Hour
)

var ErrNoSigningKey = errors.New("signing key not configured")

// SetSigningKey sets the HMAC key (derived from the vault master key).
func (s *Service) SetSigningKey(k []byte) {
	m := hmac.New(sha256.New, k)
	m.Write([]byte("yimeng-sign:v1"))
	s.signKey = m.Sum(nil)
}

func (s *Service) mac(parts ...string) []byte {
	m := hmac.New(sha256.New, s.signKey)
	m.Write([]byte(strings.Join(parts, "\x00")))
	return m.Sum(nil)[:18]
}

// IssueAccess returns a short-lived access token for a device.
func (s *Service) IssueAccess(deviceID string) (string, time.Time, error) {
	if len(s.signKey) == 0 {
		return "", time.Time{}, ErrNoSigningKey
	}
	exp := s.now().Add(AccessTTL).Truncate(time.Second)
	e := strconv.FormatInt(exp.Unix(), 10)
	body := base64.RawURLEncoding.EncodeToString([]byte(deviceID + "." + e))
	return accessPrefix + body + "." + base64.RawURLEncoding.EncodeToString(s.mac("access", deviceID, e)), exp, nil
}

func (s *Service) verifyAccess(tok string) (string, bool) {
	if len(s.signKey) == 0 || len(tok) > 256 {
		return "", false
	}
	body, sig, ok := strings.Cut(strings.TrimPrefix(tok, accessPrefix), ".")
	if !ok {
		return "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return "", false
	}
	id, e, ok := strings.Cut(string(raw), ".")
	got, err2 := base64.RawURLEncoding.DecodeString(sig)
	if !ok || err2 != nil || !hmac.Equal(got, s.mac("access", id, e)) {
		return "", false
	}
	exp, err := strconv.ParseInt(e, 10, 64)
	if err != nil || s.now().Unix() >= exp {
		return "", false
	}
	return id, true
}

// Session is what login and refresh return.
type Session struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"` // seconds, of the access token
	TokenType    string `json:"token_type"`
}

// NewSession wraps a fresh refresh token with an access token.
func (s *Service) NewSession(deviceID, refresh string) (Session, error) {
	at, _, err := s.IssueAccess(deviceID)
	if err != nil {
		return Session{}, err
	}
	return Session{AccessToken: at, RefreshToken: refresh, ExpiresIn: int(AccessTTL / time.Second), TokenType: "Bearer"}, nil
}

// Refresh rotates a refresh token: the presented one stops working.
func (s *Service) Refresh(ctx context.Context, refresh string) (Device, Session, error) {
	if !strings.HasPrefix(refresh, tokenPrefix) || len(refresh) > 128 {
		return Device{}, Session{}, ErrUnauthorized
	}
	old := HashToken(refresh)
	d, err := s.loadDevice(ctx, "p.token_hash=?", old, old)
	if err != nil {
		return Device{}, Session{}, ErrUnauthorized
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return Device{}, Session{}, err
	}
	next := tokenPrefix + base64.RawURLEncoding.EncodeToString(raw[:])
	res, err := s.db.ExecContext(ctx, `UPDATE players SET token_hash=? WHERE id=? AND token_hash=?`, HashToken(next), d.ID, old)
	if err != nil {
		return Device{}, Session{}, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return Device{}, Session{}, ErrUnauthorized // lost a concurrent rotation
	}
	sess, err := s.NewSession(d.ID, next)
	return d, sess, err
}

// SignURL appends exp/dev/sig to path (which may carry a query) so the URL
// works without a header for ttl. The signature covers the path only.
func (s *Service) SignURL(path, deviceID string, ttl time.Duration) string {
	if len(s.signKey) == 0 || deviceID == "" {
		return path
	}
	if ttl <= 0 {
		ttl = SignedURLTTL
	}
	p, _, _ := strings.Cut(path, "?")
	p = canonicalPath(p)
	// round expiry so URLs stay stable (cacheable) for a while
	exp := s.now().Add(ttl).Truncate(time.Hour).Add(time.Hour).Unix()
	e := strconv.FormatInt(exp, 10)
	sig := base64.RawURLEncoding.EncodeToString(s.mac("url", p, deviceID, e))
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + "exp=" + e + "&dev=" + url.QueryEscape(deviceID) + "&sig=" + sig
}

// VerifyURL checks a signed request path + query and returns the device id.
func (s *Service) VerifyURL(path string, q url.Values) (string, bool) {
	sig, e, dev := q.Get("sig"), q.Get("exp"), q.Get("dev")
	if len(s.signKey) == 0 || sig == "" || e == "" || dev == "" {
		return "", false
	}
	exp, err := strconv.ParseInt(e, 10, 64)
	if err != nil || s.now().Unix() >= exp {
		return "", false
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(got, s.mac("url", canonicalPath(path), dev, e)) {
		return "", false
	}
	return dev, true
}

// canonicalPath folds /api/v1/x to /api/x so both prefixes verify.
func canonicalPath(p string) string {
	if strings.HasPrefix(p, "/api/v1/") {
		return "/api/" + strings.TrimPrefix(p, "/api/v1/")
	}
	return p
}
