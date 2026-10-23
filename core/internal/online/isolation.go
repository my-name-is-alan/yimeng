package online

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/owner"
	"github.com/my-name-is-alan/yimeng/core/internal/vault"
)

var (
	ErrSharedNeedsLogin = errors.New("shared_account_needs_login")
	ErrSharedRequired   = errors.New("shared_stream_required")
	ErrGrantRevoked     = errors.New("shared_stream_revoked")
	ErrIdentityRequired = errors.New("account_identity_required")
	ErrGrantConflict    = errors.New("stream_grant_conflict")
)

type scopeKey struct{}
type heldScopeKey struct{}

func FromContext(ctx context.Context) *Service { s, _ := ctx.Value(scopeKey{}).(*Service); return s }

type streamRuntime struct {
	plugin Plugin
	client *Client
}

type userScope struct {
	gate          chan struct{}
	svc           *Service
	configVersion uint64
	version       int64
}

// Accounts owns personal plugin runtimes. Each user's operations are serialized;
// audio requests use independent, immutable snapshots of the granted account.
// No request ever switches the server-wide default account to impersonate a user.
type Accounts struct {
	base          *Service
	db            *sql.DB
	mu            sync.Mutex
	users         map[string]*userScope
	config        atomic.Uint64
	migration     sync.Mutex
	migrationDone bool
	activeMu      sync.Mutex
	active        map[*streamPermit]map[uint64]context.CancelFunc
	next          uint64
	leaseMu       sync.Mutex
	leases        map[string]Resolution
	qrMu          sync.Mutex
	qr            map[string]time.Time
	public        atomic.Pointer[Service]
	runtimeMu     sync.Mutex
	runtimes      map[string]*streamRuntime
}

func NewAccounts(ctx context.Context, base *Service) (*Accounts, error) {
	a := &Accounts{base: base, db: base.Reg.db, users: map[string]*userScope{},
		active: map[*streamPermit]map[uint64]context.CancelFunc{}, leases: map[string]Resolution{}, qr: map[string]time.Time{}, runtimes: map[string]*streamRuntime{}}
	var configVersion uint64
	if err := a.db.QueryRowContext(ctx, `SELECT version FROM online_config_version WHERE singleton=1`).Scan(&configVersion); err != nil {
		return nil, err
	}
	a.config.Store(configVersion)
	if err := a.MigrateLegacy(ctx, ""); err != nil {
		return nil, err
	}
	return a, nil
}

func namespace(uid string) string {
	h := sha256.Sum256([]byte(uid))
	return "u" + hex.EncodeToString(h[:16])
}

type personalVault struct {
	a   *Accounts
	uid string
}

func (v personalVault) key(source string) string {
	h := sha256.Sum256([]byte(v.uid + "\x00" + source))
	return "personal:" + hex.EncodeToString(h[:24])
}
func (v personalVault) Get(ctx context.Context, source string) (vault.Credential, error) {
	if v.uid == "" {
		return vault.Credential{}, vault.ErrNotFound
	}
	return v.a.base.Reg.vault.Get(ctx, v.key(source))
}
func (v personalVault) Put(ctx context.Context, source string, c vault.Credential) error {
	if v.uid == "" {
		return nil
	}
	// Recording the reference first also makes a failed write removable on deletion.
	if _, err := v.a.db.ExecContext(ctx, `INSERT INTO online_secret_refs(user_id,source,vault_key) VALUES(?,?,?)
		ON CONFLICT(user_id,source) DO NOTHING`, v.uid, source, v.key(source)); err != nil {
		return err
	}
	return v.a.base.Reg.vault.Put(ctx, v.key(source), c)
}
func (v personalVault) Delete(ctx context.Context, source string) error {
	if v.uid == "" {
		return nil
	}
	return v.a.base.Reg.vault.Delete(ctx, v.key(source))
}

func (a *Accounts) role(ctx context.Context, uid string) (string, error) {
	var role string
	if uid == "" {
		return "", ErrIdentityRequired
	}
	if err := a.db.QueryRowContext(ctx, `SELECT role FROM users WHERE id=?`, uid).Scan(&role); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrIdentityRequired
		}
		return "", err
	}
	return role, nil
}

func (a *Accounts) scope(uid string) *userScope {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.users[uid]
	if s == nil {
		s = &userScope{gate: make(chan struct{}, 1)}
		a.users[uid] = s
	}
	return s
}

func lockScope(ctx context.Context, s *userScope) error {
	select {
	case s.gate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *Accounts) newRegistry(ctx context.Context, uid string) (*Registry, error) {
	r := NewRegistry(a.db, personalVault{a, uid}, a.base.Reg.log, a.base.Reg.hc)
	r.namespace = namespace(uid)
	a.base.Reg.mu.Lock()
	var factories []Factory
	for _, id := range a.base.Reg.order {
		factories = append(factories, a.base.Reg.inst[id].factory)
	}
	a.base.Reg.mu.Unlock()
	for _, f := range factories {
		if err := r.Register(f); err != nil {
			return nil, err
		}
	}
	if err := r.Load(ctx); err != nil {
		return nil, err
	}
	return r, nil
}

// Acquire scopes a finite personal-data request. Caller must release it. SWR
// refreshes are drained before changing identity so old cookie updates cannot
// write into a newly selected account. Stream/SSE/WS handlers do not hold this gate.
func (a *Accounts) Acquire(ctx context.Context, mutate bool) (context.Context, func(), error) {
	uid := owner.From(ctx).ID
	if _, err := a.role(ctx, uid); err != nil {
		return ctx, nil, err
	}
	if err := a.MigrateLegacy(ctx, ""); err != nil {
		return ctx, nil, err
	}
	s := a.scope(uid)
	if err := lockScope(ctx, s); err != nil {
		return ctx, nil, err
	}
	release := func() { <-s.gate }
	if _, err := a.db.ExecContext(ctx, `INSERT INTO online_owner_state(user_id,namespace) VALUES(?,?) ON CONFLICT(user_id) DO NOTHING`, uid, namespace(uid)); err != nil {
		release()
		return ctx, nil, err
	}
	var version int64
	if err := a.db.QueryRowContext(ctx, `SELECT version FROM online_owner_state WHERE user_id=?`, uid).Scan(&version); err != nil {
		release()
		return ctx, nil, err
	}
	if mutate {
		if s.svc != nil {
			s.svc.Cache.Wait()
		}
		a.activeMu.Lock()
		err := a.db.QueryRowContext(ctx, `UPDATE online_owner_state SET version=version+1 WHERE user_id=? RETURNING version`, uid).Scan(&version)

		a.activeMu.Unlock()
		if err != nil {
			release()
			return ctx, nil, err
		}
	}
	if s.svc == nil || s.configVersion != a.config.Load() {
		if s.svc != nil {
			s.svc.Cache.Wait()
		}
		r, err := a.newRegistry(ctx, uid)
		if err != nil {
			release()
			return ctx, nil, err
		}
		s.svc = NewService(r, a.base.Lib, a.cache(uid, version), a.base.Log)
		s.configVersion = a.config.Load()
	} else if version != s.version {
		s.svc = NewService(s.svc.Reg, a.base.Lib, a.cache(uid, version), a.base.Log)
	}
	s.version = version
	s.svc.Isolated, s.svc.Resolver = true, a.Resolve
	ctx = context.WithValue(ctx, scopeKey{}, s.svc)
	ctx = context.WithValue(ctx, heldScopeKey{}, uid)
	return ctx, release, nil
}

func (a *Accounts) cache(uid string, version int64) *Cache {
	c := NewCache(a.db)
	c.namespace = fmt.Sprintf("%s/%d-%d", namespace(uid), version, a.config.Load())
	if a.base.Cache != nil {
		c.MaxBytes = a.base.Cache.MaxBytes
	}
	return c
}

// Public returns a session-free runtime. It is also the lightweight stream
// facade; Resolve still requires the authenticated request owner.
func (a *Accounts) Public(ctx context.Context) (*Service, error) {
	if s := a.public.Load(); s != nil {
		return s, nil
	}
	return a.buildPublic(ctx)
}

func (a *Accounts) buildPublic(ctx context.Context) (*Service, error) {
	r, err := a.newRegistry(ctx, "")
	if err != nil {
		return nil, err
	}
	s := NewService(r, a.base.Lib, a.cache("", 1), a.base.Log)
	s.Isolated, s.Resolver = true, a.Resolve
	a.public.Store(s)
	return s, nil
}

func (a *Accounts) ConfigChanged() error {
	var version uint64
	if err := a.db.QueryRow(`UPDATE online_config_version SET version=version+1 WHERE singleton=1 RETURNING version`).Scan(&version); err != nil {
		return err
	}
	a.config.Store(version)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := a.buildPublic(ctx); err != nil {
		a.base.Log.Error("refresh public plugin configuration", "err", err)
		return err
	}
	a.leaseMu.Lock()
	clear(a.leases)
	a.leaseMu.Unlock()
	a.runtimeMu.Lock()
	clear(a.runtimes)
	a.runtimeMu.Unlock()
	return nil
}

func (a *Accounts) Materialize(ctx context.Context, ids []string) error {
	if s := FromContext(ctx); s != nil {
		return s.Materialize(ctx, ids)
	}
	c, release, err := a.Acquire(ctx, false)
	if err != nil {
		return err
	}
	defer release()
	return FromContext(c).Materialize(c, ids)
}

func (a *Accounts) RememberQR(uid, platform, key string, expiry time.Time) {
	a.qrMu.Lock()
	defer a.qrMu.Unlock()
	now := time.Now()
	for k, t := range a.qr {
		if !t.After(now) {
			delete(a.qr, k)
		}
	}
	if expiry.IsZero() || expiry.After(now.Add(5*time.Minute)) {
		expiry = now.Add(5 * time.Minute)
	}
	a.qr[uid+"\x00"+platform+"\x00"+key] = expiry
}
func (a *Accounts) OwnQR(uid, platform, key string) bool {
	a.qrMu.Lock()
	defer a.qrMu.Unlock()
	return a.qr[uid+"\x00"+platform+"\x00"+key].After(time.Now())
}
