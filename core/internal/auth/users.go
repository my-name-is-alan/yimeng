package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"golang.org/x/crypto/argon2"
)

var (
	ErrBadCredentials = errors.New("invalid username or password")
	ErrSetupDone      = errors.New("an account already exists")
	ErrWeakPassword   = errors.New("password too weak")
	ErrUserExists     = errors.New("username already taken")
	ErrInvalidUser    = errors.New("invalid username")
)

const (
	RoleAdmin = "admin"
	RoleUser  = "user"

	loginIPFailLimit   = 8 // per IP per minute
	loginUserFailLimit = 5 // per username per minute
)

// argon2id parameters (OWASP: m=19 MiB, t=2, p=1); stored in the hash string.
var argonT, argonM, argonP uint32 = 2, 19 * 1024, 1

type User struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Nickname  string    `json:"nickname"`             // defaults to username
	AvatarURL string    `json:"avatar_url,omitempty"` // uploaded avatar, if any
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

const userCols = `u.id, u.username, u.nickname, u.role, u.created_at, COALESCE(a.updated_at, 0)`
const userFrom = ` FROM users u LEFT JOIN user_avatars a ON a.user_id=u.id`

func (u *User) finish(created, avatarAt int64) {
	u.CreatedAt = time.UnixMilli(created).UTC()
	if u.Nickname == "" {
		u.Nickname = u.Username
	}
	if avatarAt > 0 {
		u.AvatarURL = fmt.Sprintf("/api/v1/users/%s/avatar?v=%d", u.ID, avatarAt)
	}
}

// HashPassword returns an argon2id PHC string.
func HashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	k := argon2.IDKey([]byte(pw), salt, argonT, argonM, uint8(argonP), 32)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonM, argonT, argonP,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(k)), nil
}

// CheckPassword verifies pw against a PHC string in constant time.
func CheckPassword(hash, pw string) bool {
	p := strings.Split(hash, "$")
	if len(p) != 6 || p[1] != "argon2id" {
		return false
	}
	var m, t uint32
	var par uint8
	if _, err := fmt.Sscanf(p[3], "m=%d,t=%d,p=%d", &m, &t, &par); err != nil || m == 0 || m > 1<<20 || t == 0 || t > 10 || par == 0 {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(p[4])
	want, err2 := base64.RawStdEncoding.DecodeString(p[5])
	if err1 != nil || err2 != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, t, m, par, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

var common = map[string]bool{"password": true, "12345678": true, "123456789": true, "1234567890": true, "qwerty123": true,
	"password1": true, "iloveyou": true, "admin123": true, "admin1234": true, "11111111": true, "88888888": true,
	"abc12345": true, "qwertyuiop": true, "1qaz2wsx": true, "yimeng123": true, "a1234567": true, "woaini1314": true}

// CheckStrength enforces ≥10 characters, at least 3 of 4 character classes
// (or ≥16 characters), not a common password and not containing the username.
func CheckStrength(username, pw string) error {
	if len([]rune(pw)) < 10 {
		return fmt.Errorf("%w: at least 10 characters", ErrWeakPassword)
	}
	if len(pw) > 256 {
		return fmt.Errorf("%w: at most 256 bytes", ErrWeakPassword)
	}
	low := strings.ToLower(pw)
	if common[low] {
		return fmt.Errorf("%w: too common", ErrWeakPassword)
	}
	if u := strings.ToLower(strings.TrimSpace(username)); len(u) >= 3 && strings.Contains(low, u) {
		return fmt.Errorf("%w: must not contain the username", ErrWeakPassword)
	}
	var lo, up, dg, sy bool
	distinct := map[rune]bool{}
	for _, r := range pw {
		distinct[r] = true
		switch {
		case unicode.IsLower(r):
			lo = true
		case unicode.IsUpper(r):
			up = true
		case unicode.IsDigit(r):
			dg = true
		default:
			sy = true
		}
	}
	classes := 0
	for _, b := range []bool{lo, up, dg, sy} {
		if b {
			classes++
		}
	}
	if len(distinct) < 5 {
		return fmt.Errorf("%w: too repetitive", ErrWeakPassword)
	}
	if classes < 3 && len([]rune(pw)) < 16 {
		return fmt.Errorf("%w: use 3 of lowercase, uppercase, digits, symbols (or 16+ characters)", ErrWeakPassword)
	}
	return nil
}

func validUsername(u string) bool {
	if l := len([]rune(u)); l < 2 || l > 32 {
		return false
	}
	for _, r := range u {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}

func newID(prefix string) string {
	var b [12]byte
	rand.Read(b[:])
	return prefix + hex.EncodeToString(b[:])
}

// UserCount reports how many web accounts exist (0 = first-run setup).
func (s *Service) UserCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// CreateUser adds an account. With firstRun it only succeeds while no users
// exist and the account is always admin (atomic: the INSERT checks emptiness).
func (s *Service) CreateUser(ctx context.Context, username, pw, role string, firstRun bool) (User, error) {
	username = strings.TrimSpace(username)
	if !validUsername(username) {
		return User{}, ErrInvalidUser
	}
	if role != RoleAdmin && role != RoleUser {
		return User{}, fmt.Errorf("invalid role")
	}
	if err := CheckStrength(username, pw); err != nil {
		return User{}, err
	}
	h, err := HashPassword(pw)
	if err != nil {
		return User{}, err
	}
	now := s.now()
	u := User{ID: newID("usr_"), Username: username, Role: role, CreatedAt: now.UTC().Truncate(time.Millisecond)}
	q := `INSERT INTO users(id, username, pw_hash, role, created_at, updated_at) SELECT ?,?,?,?,?,? WHERE NOT EXISTS (SELECT 1 FROM users WHERE username=? COLLATE NOCASE)`
	args := []any{u.ID, u.Username, h, u.Role, now.UnixMilli(), now.UnixMilli(), username}
	if firstRun {
		u.Role = RoleAdmin
		args[3] = RoleAdmin
		q += ` AND NOT EXISTS (SELECT 1 FROM users)`
	}
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return User{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if firstRun {
			if c, _ := s.UserCount(ctx); c > 0 {
				return User{}, ErrSetupDone
			}
		}
		return User{}, ErrUserExists
	}
	return u, nil
}

func (s *Service) loginLockedLocked(ip, user string, now time.Time) bool {
	return s.isLockedLocked("login:"+ip, now) || s.isLockedLocked("user:"+strings.ToLower(user), now)
}

// Login checks credentials (rate-limited per IP and per username, with a
// 15-minute lockout) and issues a device token for the browser/app session.
func (s *Service) Login(ctx context.Context, ip, username, pw, deviceName, deviceType string) (Device, User, string, error) {
	username = strings.TrimSpace(username)
	s.mu.Lock()
	now := s.now()
	if s.loginLockedLocked(ip, username, now) || s.isLockedLocked("*", now) {
		s.mu.Unlock()
		return Device{}, User{}, "", ErrRateLimited
	}
	s.mu.Unlock()

	var u User
	var h string
	var created int64
	err := s.db.QueryRowContext(ctx, `SELECT id, username, pw_hash, role, created_at FROM users WHERE username=? COLLATE NOCASE`, username).
		Scan(&u.ID, &u.Username, &h, &u.Role, &created)
	ok := err == nil && CheckPassword(h, pw)
	if err != nil {
		// equalize timing for unknown users
		CheckPassword("$argon2id$v=19$m=19456,t=2,p=1$c2FsdHNhbHRzYWx0c2FsdA$aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaGhhc2g", pw)
	}
	if !ok {
		s.mu.Lock()
		if bump(s.fails, "login:"+ip, now) >= loginIPFailLimit {
			s.lockLocked("login:"+ip, now, "too many failed logins")
			delete(s.fails, "login:"+ip)
		}
		uk := "user:" + strings.ToLower(username)
		if bump(s.fails, uk, now) >= loginUserFailLimit {
			s.lockLocked(uk, now, "too many failed logins for one username")
			delete(s.fails, uk)
		}
		s.mu.Unlock()
		return Device{}, User{}, "", ErrBadCredentials
	}
	s.mu.Lock()
	delete(s.fails, "login:"+ip)
	delete(s.fails, "user:"+strings.ToLower(username))
	s.mu.Unlock()
	u.CreatedAt = time.UnixMilli(created).UTC()
	if deviceName = strings.TrimSpace(deviceName); deviceName == "" {
		deviceName = "网页"
	}
	if deviceType == "" {
		deviceType = "web"
	}
	d, tok, err := s.issue(ctx, deviceName+" · "+u.Username, deviceType, u)
	return d, u, tok, err
}

func (s *Service) issue(ctx context.Context, name, deviceType string, u User) (Device, string, error) {
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
	now := s.now()
	d := Device{ID: newID("dev_"), Name: name, DeviceType: deviceType, CreatedAt: now.UTC().Truncate(time.Millisecond),
		Admin: u.Role == RoleAdmin, UserID: u.ID}
	d.LastSeenAt = d.CreatedAt
	_, err := s.db.ExecContext(ctx, `INSERT INTO players(id, name, device_type, token_hash, created_at, last_seen_at, is_admin, user_id) VALUES (?,?,?,?,?,?,?,?)`,
		d.ID, d.Name, deviceType, HashToken(token), now.UnixMilli(), now.UnixMilli(), d.Admin, u.ID)
	return d, token, err
}

func (s *Service) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+userCols+userFrom+` ORDER BY u.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		var u User
		var c, av int64
		if err := rows.Scan(&u.ID, &u.Username, &u.Nickname, &u.Role, &c, &av); err != nil {
			return nil, err
		}
		u.finish(c, av)
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Service) adminCountExcept(ctx context.Context, tx *sql.Tx, id string) int {
	var n int
	tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role='admin' AND id<>?`, id).Scan(&n)
	return n
}

// SetRole changes a user's role and the admin flag of all their sessions.
func (s *Service) SetRole(ctx context.Context, id, role string) error {
	if role != RoleAdmin && role != RoleUser {
		return fmt.Errorf("invalid role")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if role == RoleUser && s.adminCountExcept(ctx, tx, id) == 0 {
		return ErrLastAdmin
	}
	res, err := tx.ExecContext(ctx, `UPDATE users SET role=?, updated_at=? WHERE id=?`, role, s.now().UnixMilli(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `UPDATE players SET is_admin=? WHERE user_id=?`, role == RoleAdmin, id); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteUser removes an account and every session it created.
func (s *Service) DeleteUser(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if s.adminCountExcept(ctx, tx, id) == 0 {
		var role string
		if tx.QueryRowContext(ctx, `SELECT role FROM users WHERE id=?`, id).Scan(&role); role == RoleAdmin {
			return ErrLastAdmin
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM players WHERE user_id=?`, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// SetPassword replaces a password (after a strength check) and revokes the
// user's other sessions (keepDevice stays signed in; "" revokes all).
func (s *Service) SetPassword(ctx context.Context, id, pw, keepDevice string) error {
	var name string
	if err := s.db.QueryRowContext(ctx, `SELECT username FROM users WHERE id=?`, id).Scan(&name); err != nil {
		return ErrNotFound
	}
	if err := CheckStrength(name, pw); err != nil {
		return err
	}
	h, err := HashPassword(pw)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE users SET pw_hash=?, updated_at=? WHERE id=?`, h, s.now().UnixMilli(), id); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM players WHERE user_id=? AND id<>?`, id, keepDevice)
	return err
}

// ChangePassword verifies the old password first.
func (s *Service) ChangePassword(ctx context.Context, id, oldPw, newPw, keepDevice string) error {
	var h string
	if err := s.db.QueryRowContext(ctx, `SELECT pw_hash FROM users WHERE id=?`, id).Scan(&h); err != nil {
		return ErrNotFound
	}
	if !CheckPassword(h, oldPw) {
		return ErrBadCredentials
	}
	return s.SetPassword(ctx, id, newPw, keepDevice)
}

// ResetPassword is the CLI escape hatch: sets (or creates, as admin) the
// account and revokes all of its sessions.
func (s *Service) ResetPassword(ctx context.Context, username, pw string) (created bool, err error) {
	var id string
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM users WHERE username=? COLLATE NOCASE`, strings.TrimSpace(username)).Scan(&id); err == nil {
		return false, s.SetPassword(ctx, id, pw, "")
	}
	_, err = s.CreateUser(ctx, username, pw, RoleAdmin, false)
	return err == nil, err
}

// UserByID loads an account.
func (s *Service) UserByID(ctx context.Context, id string) (User, error) {
	var u User
	var c, av int64
	err := s.db.QueryRowContext(ctx, `SELECT `+userCols+userFrom+` WHERE u.id=?`, id).Scan(&u.ID, &u.Username, &u.Nickname, &u.Role, &c, &av)
	if err != nil {
		return User{}, ErrNotFound
	}
	u.finish(c, av)
	return u, nil
}

// RemoveAllUsers deletes every web account and its sessions (YIMENG_ADMIN_RESET).
// Paired app devices are kept.
func (s *Service) RemoveAllUsers(ctx context.Context) (int, error) {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM players WHERE user_id IS NOT NULL`); err != nil {
		return 0, err
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM users`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// SetNickname sets the display name ("" = use the username).
func (s *Service) SetNickname(ctx context.Context, id, nick string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET nickname=?, updated_at=? WHERE id=?`, nick, s.now().UnixMilli(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetAvatar stores an already validated avatar image.
func (s *Service) SetAvatar(ctx context.Context, id, mime string, data []byte) error {
	if _, err := s.UserByID(ctx, id); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO user_avatars(user_id, mime, data, updated_at) VALUES (?,?,?,?)
		ON CONFLICT(user_id) DO UPDATE SET mime=excluded.mime, data=excluded.data, updated_at=excluded.updated_at`, id, mime, data, s.now().UnixMilli())
	return err
}

// Avatar returns the stored avatar.
func (s *Service) Avatar(ctx context.Context, id string) (string, []byte, time.Time, error) {
	var mime string
	var data []byte
	var at int64
	if err := s.db.QueryRowContext(ctx, `SELECT mime, data, updated_at FROM user_avatars WHERE user_id=?`, id).Scan(&mime, &data, &at); err != nil {
		return "", nil, time.Time{}, ErrNotFound
	}
	return mime, data, time.UnixMilli(at).UTC(), nil
}

func (s *Service) DeleteAvatar(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM user_avatars WHERE user_id=?`, id)
	return err
}
