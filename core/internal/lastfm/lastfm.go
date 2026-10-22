// Package lastfm implements optional per-account Last.fm scrobbling (D56).
//
// Each account brings its own API key/secret. They and the session key are
// stored only in the Vault (entry "lastfm:<user id>") and are never returned
// to clients or logged. Plays are queued in lastfm_queue and sent in batches
// by Run; failures are retried with backoff.
package lastfm

import (
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/vault"
)

const (
	DefaultAPI  = "https://ws.audioscrobbler.com/2.0/"
	DefaultAuth = "https://www.last.fm/api/auth/"
	maxAttempts = 8
	batchSize   = 50
)

var (
	ErrNoAccount   = errors.New("lastfm: an account is required")
	ErrNoKeys      = errors.New("lastfm: api key/secret not set")
	ErrNoToken     = errors.New("lastfm: start authorization first")
	ErrNotAuthed   = errors.New("lastfm: authorization not granted yet")
	ErrInvalidKeys = errors.New("lastfm: invalid api key/secret")
)

type Service struct {
	DB      *sql.DB
	Vault   vault.Vault
	Log     *slog.Logger
	HTTP    *http.Client
	APIURL  string // default DefaultAPI
	AuthURL string // default DefaultAuth
	Now     func() time.Time
	// MinTrackMs: shorter tracks are not scrobbled (Last.fm rule, 30 s).
	// 0 = default; negative disables (tests).
	MinTrackMs int64
}

// Status is what clients may see: never keys or session.
type Status struct {
	Configured bool   `json:"configured"` // key + secret saved
	Connected  bool   `json:"connected"`  // session obtained
	Enabled    bool   `json:"enabled"`
	Username   string `json:"username,omitempty"`
	Pending    int    `json:"pending"` // queued scrobbles
}

func entry(userID string) string { return "lastfm:" + userID }

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) cred(ctx context.Context, userID string) (map[string]string, error) {
	if userID == "" {
		return nil, ErrNoAccount
	}
	c, err := s.Vault.Get(ctx, entry(userID))
	if errors.Is(err, vault.ErrNotFound) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	if c.Fields == nil {
		c.Fields = map[string]string{}
	}
	return c.Fields, nil
}

func (s *Service) save(ctx context.Context, userID string, f map[string]string) error {
	return s.Vault.Put(ctx, entry(userID), vault.Credential{Method: vault.LoginAPIKey, Fields: f})
}

func (s *Service) Status(ctx context.Context, userID string) (Status, error) {
	f, err := s.cred(ctx, userID)
	if err != nil {
		return Status{}, err
	}
	st := Status{Configured: f["api_key"] != "" && f["api_secret"] != "", Connected: f["session_key"] != "",
		Enabled: f["session_key"] != "" && f["enabled"] != "0", Username: f["username"]}
	_ = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM lastfm_queue WHERE user_id=?`, userID).Scan(&st.Pending)
	return st, nil
}

// SetKeys stores the account's API key/secret (replacing any session).
func (s *Service) SetKeys(ctx context.Context, userID, key, secret string) error {
	key, secret = strings.TrimSpace(key), strings.TrimSpace(secret)
	if !hex32(key) || !hex32(secret) {
		return ErrInvalidKeys
	}
	if userID == "" {
		return ErrNoAccount
	}
	return s.save(ctx, userID, map[string]string{"api_key": key, "api_secret": secret})
}

func hex32(v string) bool {
	if len(v) != 32 {
		return false
	}
	_, err := hex.DecodeString(v)
	return err == nil
}

// SetEnabled pauses/resumes scrobbling without dropping the session.
func (s *Service) SetEnabled(ctx context.Context, userID string, on bool) error {
	f, err := s.cred(ctx, userID)
	if err != nil {
		return err
	}
	if f["session_key"] == "" {
		return ErrNotAuthed
	}
	f["enabled"] = map[bool]string{true: "1", false: "0"}[on]
	return s.save(ctx, userID, f)
}

// Disconnect removes keys, session and queued scrobbles.
func (s *Service) Disconnect(ctx context.Context, userID string) error {
	if userID == "" {
		return ErrNoAccount
	}
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM lastfm_queue WHERE user_id=?`, userID)
	if err := s.Vault.Delete(ctx, entry(userID)); err != nil && !errors.Is(err, vault.ErrNotFound) {
		return err
	}
	return nil
}

// StartAuth fetches a request token and returns the URL the user must open
// to grant access (desktop auth flow). The token is kept in the Vault.
func (s *Service) StartAuth(ctx context.Context, userID string) (string, error) {
	f, err := s.cred(ctx, userID)
	if err != nil {
		return "", err
	}
	if f["api_key"] == "" || f["api_secret"] == "" {
		return "", ErrNoKeys
	}
	var r struct {
		Token string `json:"token"`
	}
	if err := s.call(ctx, http.MethodGet, f, map[string]string{"method": "auth.getToken"}, &r); err != nil {
		return "", err
	}
	if r.Token == "" {
		return "", errors.New("lastfm: empty token")
	}
	f["token"] = r.Token
	delete(f, "session_key")
	delete(f, "username")
	if err := s.save(ctx, userID, f); err != nil {
		return "", err
	}
	base := s.AuthURL
	if base == "" {
		base = DefaultAuth
	}
	return base + "?" + url.Values{"api_key": {f["api_key"]}, "token": {r.Token}}.Encode(), nil
}

// CompleteAuth exchanges the granted token for a session key.
func (s *Service) CompleteAuth(ctx context.Context, userID string) (Status, error) {
	f, err := s.cred(ctx, userID)
	if err != nil {
		return Status{}, err
	}
	if f["token"] == "" {
		return Status{}, ErrNoToken
	}
	var r struct {
		Session struct {
			Name string `json:"name"`
			Key  string `json:"key"`
		} `json:"session"`
	}
	if err := s.call(ctx, http.MethodGet, f, map[string]string{"method": "auth.getSession", "token": f["token"]}, &r); err != nil {
		var ae *APIError
		if errors.As(err, &ae) && (ae.Code == 14 || ae.Code == 15 || ae.Code == 4) {
			return Status{}, ErrNotAuthed
		}
		return Status{}, err
	}
	if r.Session.Key == "" {
		return Status{}, ErrNotAuthed
	}
	delete(f, "token")
	f["session_key"], f["username"], f["enabled"] = r.Session.Key, r.Session.Name, "1"
	if err := s.save(ctx, userID, f); err != nil {
		return Status{}, err
	}
	return s.Status(ctx, userID)
}

// Enqueue queues a qualifying play for scrobbling if the account is connected.
func (s *Service) Enqueue(ctx context.Context, userID, trackID string, playedAt time.Time) {
	if userID == "" {
		return
	}
	f, err := s.cred(ctx, userID)
	if err != nil || f["session_key"] == "" || f["enabled"] == "0" {
		return
	}
	_, _ = s.DB.ExecContext(ctx, `INSERT INTO lastfm_queue(user_id, track_id, played_at) VALUES (?,?,?)`, userID, trackID, playedAt.UnixMilli())
}

// Run flushes the queue periodically until ctx ends.
func (s *Service) Run(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = time.Minute
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		s.Flush(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

type queued struct {
	id                   int64
	user, track          string
	at                   int64
	attempts             int
	title, artist, album string
	durMs                int64
}

// Flush sends due scrobbles, batched per account.
func (s *Service) Flush(ctx context.Context) {
	now := s.now().UnixMilli()
	rows, err := s.DB.QueryContext(ctx, `SELECT q.id, q.user_id, q.track_id, q.played_at, q.attempts,
		COALESCE(t.title,''), COALESCE(t.artist_name,''), COALESCE(a.title,''), COALESCE(t.duration_ms,0)
		FROM lastfm_queue q LEFT JOIN tracks t ON t.id=q.track_id LEFT JOIN albums a ON a.id=t.album_id
		WHERE q.next_at<=? ORDER BY q.user_id, q.played_at LIMIT 500`, now)
	if err != nil {
		return
	}
	byUser := map[string][]queued{}
	var order []string
	for rows.Next() {
		var q queued
		if rows.Scan(&q.id, &q.user, &q.track, &q.at, &q.attempts, &q.title, &q.artist, &q.album, &q.durMs) == nil {
			if byUser[q.user] == nil {
				order = append(order, q.user)
			}
			byUser[q.user] = append(byUser[q.user], q)
		}
	}
	rows.Close()
	for _, u := range order {
		items := byUser[u]
		for len(items) > 0 {
			n := min(batchSize, len(items))
			s.send(ctx, u, items[:n])
			items = items[n:]
		}
	}
}

func (s *Service) send(ctx context.Context, userID string, items []queued) {
	drop := func(q queued) { _, _ = s.DB.ExecContext(ctx, `DELETE FROM lastfm_queue WHERE id=?`, q.id) }
	f, err := s.cred(ctx, userID)
	if err != nil {
		return
	}
	if f["session_key"] == "" {
		for _, q := range items {
			drop(q)
		}
		return
	}
	p := map[string]string{"method": "track.scrobble", "sk": f["session_key"]}
	var sent []queued
	for _, q := range items {
		// Last.fm rejects unknown metadata and tracks < 30 s
		minMs := s.MinTrackMs
		if minMs == 0 {
			minMs = 30000
		}
		if q.title == "" || q.artist == "" || (q.durMs > 0 && q.durMs < minMs) {
			drop(q)
			continue
		}
		i := strconv.Itoa(len(sent))
		p["artist["+i+"]"], p["track["+i+"]"], p["timestamp["+i+"]"] = q.artist, q.title, strconv.FormatInt(q.at/1000, 10)
		if q.album != "" {
			p["album["+i+"]"] = q.album
		}
		if q.durMs > 0 {
			p["duration["+i+"]"] = strconv.FormatInt(q.durMs/1000, 10)
		}
		sent = append(sent, q)
	}
	if len(sent) == 0 {
		return
	}
	err = s.call(ctx, http.MethodPost, f, p, nil)
	var ae *APIError
	switch {
	case err == nil:
		for _, q := range sent {
			drop(q)
		}
	case errors.As(err, &ae) && (ae.Code == 9 || ae.Code == 10 || ae.Code == 26):
		// session/key revoked: keep the keys, drop the session; user must re-auth
		delete(f, "session_key")
		_ = s.save(ctx, userID, f)
		s.logWarn("lastfm session invalid; re-authorization needed", userID, ae.Code)
	default:
		for _, q := range sent {
			if q.attempts+1 >= maxAttempts {
				drop(q)
				continue
			}
			back := time.Duration(1<<q.attempts) * time.Minute
			_, _ = s.DB.ExecContext(ctx, `UPDATE lastfm_queue SET attempts=attempts+1, next_at=? WHERE id=?`, s.now().Add(back).UnixMilli(), q.id)
		}
		code := 0
		if ae != nil {
			code = ae.Code
		}
		s.logWarn("lastfm scrobble failed; will retry", userID, code)
	}
}

func (s *Service) logWarn(msg, userID string, code int) {
	if s.Log != nil {
		s.Log.Warn(msg, "user", userID, "code", code) // never log keys or responses
	}
}

// APIError is a Last.fm error response.
type APIError struct {
	Code    int    `json:"error"`
	Message string `json:"message"`
}

func (e *APIError) Error() string { return fmt.Sprintf("lastfm error %d: %s", e.Code, e.Message) }

// Sign returns the api_sig for params (format/callback excluded).
func Sign(params map[string]string, secret string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k != "format" && k != "callback" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteString(params[k])
	}
	b.WriteString(secret)
	sum := md5.Sum([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func (s *Service) call(ctx context.Context, method string, f map[string]string, p map[string]string, out any) error {
	p["api_key"] = f["api_key"]
	p["api_sig"] = Sign(p, f["api_secret"])
	p["format"] = "json"
	v := url.Values{}
	for k, x := range p {
		v.Set(k, x)
	}
	base := s.APIURL
	if base == "" {
		base = DefaultAPI
	}
	var req *http.Request
	var err error
	if method == http.MethodPost {
		req, err = http.NewRequestWithContext(ctx, method, base, strings.NewReader(v.Encode()))
		if err == nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	} else {
		req, err = http.NewRequestWithContext(ctx, method, base+"?"+v.Encode(), nil)
	}
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "yimeng-core")
	hc := s.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	res, err := hc.Do(req)
	if err != nil {
		return errors.New("lastfm: request failed") // url carries secrets; don't wrap it
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	var ae APIError
	if json.Unmarshal(body, &ae) == nil && ae.Code != 0 {
		return &ae
	}
	if res.StatusCode/100 != 2 {
		return fmt.Errorf("lastfm: http %d", res.StatusCode)
	}
	if out != nil {
		return json.Unmarshal(body, out)
	}
	return nil
}
