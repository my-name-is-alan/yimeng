package auth

// Developer credentials (D59): personal API keys and Subsonic app passwords.

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	KindAPIKey      = "api_key"
	KindAppPassword = "app_password"
	apiKeyPrefix    = "yk_"

	ScopeRead     = "read"
	ScopePlayback = "playback"
	ScopeWrite    = "library-write"
	ScopeAdmin    = "admin"
)

var (
	AllScopes        = []string{ScopeRead, ScopePlayback, ScopeWrite, ScopeAdmin}
	ErrInvalidScope  = errors.New("invalid scope")
	ErrNoSecretStore = errors.New("app passwords need the vault")
	ErrPlainRefused  = errors.New("plaintext password not enabled for this app password")
	ErrLocked        = errors.New("too many failed attempts; try again later")
)

// SecretStore keeps app-password plaintext encrypted (the Vault).
type SecretStore interface {
	PutSecret(ctx context.Context, key, value string) error
	GetSecret(ctx context.Context, key string) (string, error)
	DeleteSecret(ctx context.Context, key string) error
}

// Credential is the public view (never the secret).
type Credential struct {
	ID         string     `json:"id"`
	UserID     string     `json:"user_id"`
	Username   string     `json:"username,omitempty"`
	Kind       string     `json:"kind"`
	Name       string     `json:"name"`
	Scopes     []string   `json:"scopes"`
	Prefix     string     `json:"prefix"`
	AllowPlain bool       `json:"allow_plain,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
}

type NewCredential struct {
	UserID     string
	Kind       string
	Name       string
	Scopes     []string
	AllowPlain bool
	TTL        time.Duration // 0 = no expiry
}

// HasScope reports whether a device may use scope. Device and session tokens
// (no Scopes) have every scope their role allows.
func (d Device) HasScope(scope string) bool {
	if d.Via == "" {
		return scope != ScopeAdmin || d.Admin
	}
	return slices.Contains(d.Scopes, scope)
}

// NormalizeScopes validates scopes; "admin" requires an admin account.
func NormalizeScopes(in []string, isAdmin bool) ([]string, error) {
	out := []string{}
	for _, s := range in {
		s = strings.ToLower(strings.TrimSpace(s))
		if !slices.Contains(AllScopes, s) {
			return nil, fmt.Errorf("%w: %q", ErrInvalidScope, s)
		}
		if s == ScopeAdmin && !isAdmin {
			return nil, fmt.Errorf("%w: admin scope needs an admin account", ErrInvalidScope)
		}
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: at least one scope required", ErrInvalidScope)
	}
	if !slices.Contains(out, ScopeRead) { // every credential can read what it acts on
		out = append([]string{ScopeRead}, out...)
	}
	slices.SortFunc(out, func(a, b string) int { return slices.Index(AllScopes, a) - slices.Index(AllScopes, b) })
	return out, nil
}

const pwAlphabet = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func randString(n int, alphabet string) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		x, _ := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		b.WriteByte(alphabet[x.Int64()])
	}
	return b.String()
}

// CreateCredential issues a new secret (returned once).
func (s *Service) CreateCredential(ctx context.Context, in NewCredential) (Credential, string, error) {
	u, err := s.UserByID(ctx, in.UserID)
	if err != nil {
		return Credential{}, "", err
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len([]rune(in.Name)) > 64 {
		return Credential{}, "", fmt.Errorf("%w: name must be 1–64 characters", ErrInvalidScope)
	}
	scopes, err := NormalizeScopes(in.Scopes, u.Role == "admin")
	if err != nil {
		return Credential{}, "", err
	}
	var secret string
	switch in.Kind {
	case KindAPIKey:
		secret = apiKeyPrefix + randString(40, pwAlphabet)
		in.AllowPlain = false
	case KindAppPassword:
		if s.Secrets == nil {
			return Credential{}, "", ErrNoSecretStore
		}
		secret = randString(24, pwAlphabet)
	default:
		return Credential{}, "", fmt.Errorf("%w: kind must be api_key or app_password", ErrInvalidScope)
	}
	id := newID("cred")
	now := s.now()
	var exp any
	if in.TTL > 0 {
		exp = now.Add(in.TTL).UnixMilli()
	}
	prefix := secret[:min(len(secret), 7)]
	if in.Kind == KindAppPassword {
		prefix = secret[:3]
		if err := s.Secrets.PutSecret(ctx, "apppw:"+id, secret); err != nil {
			return Credential{}, "", err
		}
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO api_credentials(id, user_id, kind, name, scopes, prefix, token_hash, allow_plain, created_at, expires_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`, id, in.UserID, in.Kind, in.Name, strings.Join(scopes, " "), prefix, HashToken(secret), in.AllowPlain, now.UnixMilli(), exp); err != nil {
		if in.Kind == KindAppPassword {
			_ = s.Secrets.DeleteSecret(ctx, "apppw:"+id)
		}
		return Credential{}, "", err
	}
	c, err := s.credential(ctx, id)
	return c, secret, err
}

const credCols = `c.id, c.user_id, u.username, c.kind, c.name, c.scopes, c.prefix, c.allow_plain, c.created_at, c.last_used_at, c.expires_at`

func scanCred(sc interface{ Scan(...any) error }) (Credential, error) {
	var c Credential
	var scopes string
	var created int64
	var used, exp sql.NullInt64
	if err := sc.Scan(&c.ID, &c.UserID, &c.Username, &c.Kind, &c.Name, &scopes, &c.Prefix, &c.AllowPlain, &created, &used, &exp); err != nil {
		return c, err
	}
	c.Scopes = strings.Fields(scopes)
	c.CreatedAt = time.UnixMilli(created).UTC()
	if used.Valid {
		t := time.UnixMilli(used.Int64).UTC()
		c.LastUsedAt = &t
	}
	if exp.Valid {
		t := time.UnixMilli(exp.Int64).UTC()
		c.ExpiresAt = &t
	}
	return c, nil
}

func (s *Service) credential(ctx context.Context, id string) (Credential, error) {
	c, err := scanCred(s.db.QueryRowContext(ctx, `SELECT `+credCols+` FROM api_credentials c JOIN users u ON u.id=c.user_id
		WHERE c.id=? AND c.revoked_at IS NULL`, id))
	if err != nil {
		return Credential{}, ErrNotFound
	}
	return c, nil
}

// ListCredentials lists active credentials of userID ("" = all, for admins).
func (s *Service) ListCredentials(ctx context.Context, userID string) ([]Credential, error) {
	q := `SELECT ` + credCols + ` FROM api_credentials c JOIN users u ON u.id=c.user_id WHERE c.revoked_at IS NULL`
	args := []any{}
	if userID != "" {
		q += ` AND c.user_id=?`
		args = append(args, userID)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY c.created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Credential{}
	for rows.Next() {
		c, err := scanCred(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// RevokeCredential revokes id; userID "" means any owner (admin).
func (s *Service) RevokeCredential(ctx context.Context, id, userID string) error {
	q, args := `UPDATE api_credentials SET revoked_at=? WHERE id=? AND revoked_at IS NULL`, []any{s.now().UnixMilli(), id}
	if userID != "" {
		q += ` AND user_id=?`
		args = append(args, userID)
	}
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if s.Secrets != nil {
		_ = s.Secrets.DeleteSecret(ctx, "apppw:"+id)
	}
	return nil
}

// credDevice builds the pseudo-device a credential authenticates as.
func (s *Service) credDevice(ctx context.Context, c Credential, role string) Device {
	now := s.now()
	if c.LastUsedAt == nil || now.Sub(*c.LastUsedAt) > time.Minute {
		_, _ = s.db.ExecContext(ctx, `UPDATE api_credentials SET last_used_at=? WHERE id=?`, now.UnixMilli(), c.ID)
	}
	return Device{ID: c.ID, Name: c.Name, DeviceType: c.Kind, CreatedAt: c.CreatedAt, UserID: c.UserID, Username: c.Username,
		Admin: role == "admin" && slices.Contains(c.Scopes, ScopeAdmin), Scopes: c.Scopes, Via: c.Kind}
}

func (s *Service) activeByHash(ctx context.Context, kind, hash string) (Credential, string, error) {
	var role string
	row := s.db.QueryRowContext(ctx, `SELECT `+credCols+`, u.role FROM api_credentials c JOIN users u ON u.id=c.user_id
		WHERE c.token_hash=? AND c.kind=? AND c.revoked_at IS NULL AND (c.expires_at IS NULL OR c.expires_at>?)`, hash, kind, s.now().UnixMilli())
	var c Credential
	var scopes string
	var created int64
	var used, exp sql.NullInt64
	if err := row.Scan(&c.ID, &c.UserID, &c.Username, &c.Kind, &c.Name, &scopes, &c.Prefix, &c.AllowPlain, &created, &used, &exp, &role); err != nil {
		return Credential{}, "", ErrUnauthorized
	}
	c.Scopes = strings.Fields(scopes)
	c.CreatedAt = time.UnixMilli(created).UTC()
	if used.Valid {
		t := time.UnixMilli(used.Int64).UTC()
		c.LastUsedAt = &t
	}
	return c, role, nil
}

// AuthenticateAPIKey resolves a yk_ key.
func (s *Service) AuthenticateAPIKey(ctx context.Context, key string) (Device, error) {
	if !strings.HasPrefix(key, apiKeyPrefix) || len(key) > 128 {
		return Device{}, ErrUnauthorized
	}
	c, role, err := s.activeByHash(ctx, KindAPIKey, HashToken(key))
	if err != nil {
		return Device{}, err
	}
	return s.credDevice(ctx, c, role), nil
}

// --- Subsonic password auth with app passwords (never the login password) ---

type subFailWin struct {
	n     int
	first time.Time
}

var (
	subFailMu sync.Mutex
	subFails  = map[string]*subFailWin{}
)

const (
	subFailMax    = 10
	subFailWindow = 15 * time.Minute
)

func (s *Service) subLocked(key string) bool {
	subFailMu.Lock()
	defer subFailMu.Unlock()
	f := subFails[key]
	if f == nil {
		return false
	}
	if s.now().Sub(f.first) > subFailWindow {
		delete(subFails, key)
		return false
	}
	return f.n >= subFailMax
}

// subFail records a failure; returns true when this locks the key.
func (s *Service) subFail(key string) bool {
	subFailMu.Lock()
	defer subFailMu.Unlock()
	f := subFails[key]
	if f == nil || s.now().Sub(f.first) > subFailWindow {
		f = &subFailWin{first: s.now()}
		subFails[key] = f
	}
	f.n++
	if len(subFails) > 10000 {
		for k, v := range subFails {
			if s.now().Sub(v.first) > subFailWindow {
				delete(subFails, k)
			}
		}
	}
	return f.n == subFailMax
}

// SubsonicPasswordAuth checks u + (t,s) token auth or u + p plaintext against
// the user's app passwords. ip scopes the lockout. locked reports whether this
// attempt triggered (or hit) a lockout.
func (s *Service) SubsonicPasswordAuth(ctx context.Context, username, token, salt, plain, ip string) (dev Device, locked bool, err error) {
	key := strings.ToLower(username) + "|" + ip
	if s.subLocked(key) {
		return Device{}, true, ErrLocked
	}
	fail := func(e error) (Device, bool, error) { return Device{}, s.subFail(key), e }
	if username == "" || len(username) > 64 || (token == "" && plain == "") {
		return fail(ErrUnauthorized)
	}
	if s.Secrets == nil && plain == "" {
		return fail(ErrUnauthorized)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+credCols+`, u.role FROM api_credentials c JOIN users u ON u.id=c.user_id
		WHERE u.username=? COLLATE NOCASE AND c.kind='app_password' AND c.revoked_at IS NULL AND (c.expires_at IS NULL OR c.expires_at>?)`,
		username, s.now().UnixMilli())
	if err != nil {
		return Device{}, false, err
	}
	type cand struct {
		c    Credential
		role string
	}
	var cands []cand
	for rows.Next() {
		var c Credential
		var scopes, role string
		var created int64
		var used, exp sql.NullInt64
		if rows.Scan(&c.ID, &c.UserID, &c.Username, &c.Kind, &c.Name, &scopes, &c.Prefix, &c.AllowPlain, &created, &used, &exp, &role) == nil {
			c.Scopes = strings.Fields(scopes)
			c.CreatedAt = time.UnixMilli(created).UTC()
			if used.Valid {
				t := time.UnixMilli(used.Int64).UTC()
				c.LastUsedAt = &t
			}
			cands = append(cands, cand{c, role})
		}
	}
	rows.Close()
	if plain != "" {
		h := HashToken(plain)
		for _, x := range cands {
			var stored string
			_ = s.db.QueryRowContext(ctx, `SELECT token_hash FROM api_credentials WHERE id=?`, x.c.ID).Scan(&stored)
			if subtle.ConstantTimeCompare([]byte(stored), []byte(h)) == 1 {
				if !x.c.AllowPlain {
					return fail(ErrPlainRefused)
				}
				return s.credDevice(ctx, x.c, x.role), false, nil
			}
		}
		return fail(ErrUnauthorized)
	}
	if len(salt) < 6 || len(salt) > 64 || len(token) != 32 {
		return fail(ErrUnauthorized)
	}
	want, err := hex.DecodeString(strings.ToLower(token))
	if err != nil {
		return fail(ErrUnauthorized)
	}
	for _, x := range cands {
		pw, err := s.Secrets.GetSecret(ctx, "apppw:"+x.c.ID)
		if err != nil {
			continue
		}
		sum := md5.Sum([]byte(pw + salt))
		if subtle.ConstantTimeCompare(sum[:], want) == 1 {
			return s.credDevice(ctx, x.c, x.role), false, nil
		}
	}
	return fail(ErrUnauthorized)
}
