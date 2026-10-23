package online

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/my-name-is-alan/yimeng/core/internal/owner"
	"github.com/my-name-is-alan/yimeng/core/internal/vault"
	"time"
)

type streamPermit struct {
	Consumer, Provider, Platform, Account, Grant string
	Session                                      string
	Revision                                     int64
}

func (p *streamPermit) key() string {
	return fmt.Sprintf("%q|%q|%q|%q|%q|%q|%d", p.Consumer, p.Provider, p.Platform, p.Account, p.Grant, p.Session, p.Revision)
}

func (a *Accounts) permit(ctx context.Context, platform string) (*streamPermit, error) {
	uid := owner.From(ctx).ID
	role, err := a.role(ctx, uid)
	if err != nil {
		return nil, err
	}
	p := &streamPermit{Consumer: uid, Platform: platform}
	if role == "admin" {
		p.Provider = uid
		err = a.db.QueryRowContext(ctx, `SELECT account_id FROM plugin_accounts WHERE plugin_id=? AND is_default=1`, namespace(uid)+"."+platform).Scan(&p.Account)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, accountLoginError(p)
		}
		if err != nil {
			return nil, err
		}
	} else {
		var revoked bool
		err = a.db.QueryRowContext(ctx, `SELECT id,provider_user_id,account_id,revision,revoked FROM stream_grants WHERE consumer_user_id=? AND platform=?`, uid, platform).
			Scan(&p.Grant, &p.Provider, &p.Account, &p.Revision, &revoked)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSharedRequired
		}
		if err != nil {
			return nil, err
		}
		if revoked {
			return nil, ErrGrantRevoked
		}
	}
	cred, err := (personalVault{a, p.Provider}).Get(ctx, accountVaultKey(platform, p.Account))
	if err != nil || !hasSession(cred.Fields) {
		return nil, accountLoginError(p)
	}
	p.Session = sessionTag(cred)
	return p, a.validate(ctx, p)
}

func (a *Accounts) validate(ctx context.Context, p *streamPermit) error {
	role, err := a.role(ctx, p.Consumer)
	if err != nil {
		return err
	}
	providerRole, err := a.role(ctx, p.Provider)
	if err != nil || providerRole != "admin" {
		return ErrGrantRevoked
	}
	if p.Grant == "" {
		if p.Consumer != p.Provider || role != "admin" {
			return ErrGrantRevoked
		}
	} else {
		var n int
		if err := a.db.QueryRowContext(ctx, `SELECT count(*) FROM stream_grants WHERE id=? AND provider_user_id=? AND consumer_user_id=?
			AND platform=? AND account_id=? AND revision=? AND revoked=0`, p.Grant, p.Provider, p.Consumer, p.Platform, p.Account, p.Revision).Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			return ErrGrantRevoked
		}
	}
	var n int
	err = a.db.QueryRowContext(ctx, `SELECT count(*) FROM plugin_accounts a JOIN online_owner_state s ON a.plugin_id=s.namespace||'.'||?
		WHERE s.user_id=? AND a.account_id=?`, p.Platform, p.Provider, p.Account).Scan(&n)
	if err != nil {
		return err
	}
	if n != 1 {
		return accountLoginError(p)
	}
	cred, cerr := (personalVault{a, p.Provider}).Get(ctx, accountVaultKey(p.Platform, p.Account))
	if cerr != nil || !hasSession(cred.Fields) || sessionTag(cred) != p.Session {
		return accountLoginError(p)
	}
	if _, _, err := a.base.Reg.Enabled(p.Platform); err != nil {
		return err
	}
	return nil
}

func (a *Accounts) streamPlugin(ctx context.Context, p *streamPermit) (*streamRuntime, error) {
	scope := a.scope(p.Provider)
	if held, _ := ctx.Value(heldScopeKey{}).(string); held != p.Provider {
		if err := lockScope(ctx, scope); err != nil {
			return nil, err
		}
		defer func() { <-scope.gate }()
	}
	if err := a.validate(ctx, p); err != nil {
		return nil, err
	}
	// Share rate limit/cooldown only among consumers of this exact account.
	key := fmt.Sprintf("%q|%q|%q|%q|%d", p.Provider, p.Platform, p.Account, p.Session, a.config.Load())
	a.runtimeMu.Lock()
	defer a.runtimeMu.Unlock()
	if cached := a.runtimes[key]; cached != nil {
		return cached, nil
	}
	c, err := (personalVault{a, p.Provider}).Get(ctx, accountVaultKey(p.Platform, p.Account))
	if err != nil || !hasSession(c.Fields) {
		return nil, accountLoginError(p)
	}
	a.base.Reg.mu.Lock()
	in := a.base.Reg.inst[p.Platform]
	if in == nil {
		a.base.Reg.mu.Unlock()
		return nil, ErrUnknown
	}
	f, cfg := in.factory, in.config
	a.base.Reg.mu.Unlock()
	client := NewClient(p.Platform, cfg.Int(KeyRate, 60), a.base.Reg.hc)
	// Device identity belongs to the owner's platform runtime; session cookies
	// always come from the specifically granted account, never its current default.
	if active, err := (personalVault{a, p.Provider}).Get(ctx, vaultKey(p.Platform)); err == nil {
		for k, v := range deviceFields(active.Fields) {
			client.SetCookie(k, v)
		}
	}
	for k, v := range c.Fields {
		client.SetCookie(k, v)
	}
	plugin, err := f(Env{HTTP: client, Config: cfg})
	if err != nil {
		return nil, err
	}
	if len(a.runtimes) >= 256 {
		for old := range a.runtimes {
			delete(a.runtimes, old)
			break
		}
	}
	runtime := &streamRuntime{plugin: plugin, client: client}
	a.runtimes[key] = runtime
	return runtime, nil
}

func (a *Accounts) Resolve(ctx context.Context, id string, q Quality, fresh bool) (Resolution, error) {
	platform, native, err := split(id)
	if err != nil {
		return Resolution{}, err
	}
	p, err := a.permit(ctx, platform)
	if err != nil {
		return Resolution{}, err
	}
	key := p.key() + "|" + id + "|" + string(q)
	if !fresh {
		a.leaseMu.Lock()
		r, ok := a.leases[key]
		a.leaseMu.Unlock()
		if ok && time.Now().Add(leaseSlack).Before(r.ExpiresAt) {
			return r, nil
		}
	}
	plugin, err := a.streamPlugin(ctx, p)
	if err != nil {
		return Resolution{}, err
	}
	streamer, ok := plugin.plugin.(Streamer)
	if !ok {
		return Resolution{}, ErrNotSupported
	}
	if q == "" {
		_, cfg, err := a.base.Reg.Enabled(platform)
		if err != nil {
			return Resolution{}, err
		}
		q = Quality(cfg.String(KeyQuality, string(QHigh)))
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	st, err := streamer.Resolve(cctx, native, q)
	if errors.Is(err, ErrNeedsLogin) {
		err = accountLoginError(p)
	}
	if errors.Is(err, ErrRiskControl) {
		plugin.client.Penalize()
	} else if err == nil {
		plugin.client.Reward()
	}
	if err != nil {
		return Resolution{}, err
	}
	if err := a.validate(ctx, p); err != nil {
		return Resolution{}, err
	}
	if st.ExpiresAt.IsZero() {
		st.ExpiresAt = time.Now().Add(10 * time.Minute)
	}
	r := Resolution{TrackID: id, PlayID: id, Origin: platform, Stream: st, permit: p}
	a.leaseMu.Lock()
	if len(a.leases) >= 2048 {
		for k, v := range a.leases {
			if time.Now().Add(leaseSlack).After(v.ExpiresAt) {
				delete(a.leases, k)
			}
		}
		if len(a.leases) >= 2048 {
			for k := range a.leases {
				delete(a.leases, k)
				break
			}
		}
	}
	a.leases[key] = r
	a.leaseMu.Unlock()
	return r, nil
}

// Session identity is tied to the concrete saved account, not the owner's
// current default. Switching an unrelated personal default must not stop a grant.
func sessionTag(c vault.Credential) string {
	b, _ := json.Marshal(c.Fields)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func (a *Accounts) SessionsChanged(ctx context.Context, uid string) {
	a.activeMu.Lock()
	defer a.activeMu.Unlock()
	for p, streams := range a.active {
		if p.Provider == uid && a.validate(ctx, p) != nil {
			for _, cancel := range streams {
				cancel()
			}
		}
	}
}
func (a *Accounts) cancelGrantLocked(id string) {
	for p, streams := range a.active {
		if p.Grant == id {
			for _, cancel := range streams {
				cancel()
			}
		}
	}
}

// Guard registers a stream before fetching any bytes and atomically checks
// revocation. The periodic check covers user deletion/role changes as well.
func (a *Accounts) Guard(ctx context.Context, r Resolution) (context.Context, func(), error) {
	if r.permit == nil {
		return ctx, nil, ErrGrantRevoked
	}
	p := r.permit
	a.activeMu.Lock()
	if err := a.validate(ctx, p); err != nil {
		a.activeMu.Unlock()
		return ctx, nil, err
	}
	c, cancel := context.WithCancel(ctx)
	a.next++
	id := a.next
	if a.active[p] == nil {
		a.active[p] = map[uint64]context.CancelFunc{}
	}
	a.active[p][id] = cancel
	a.activeMu.Unlock()
	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-c.Done():
				return
			case <-tick.C:
				check, done := context.WithTimeout(c, 2*time.Second)
				err := a.validate(check, p)
				done()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()
	return c, func() {
		cancel()
		a.activeMu.Lock()
		delete(a.active[p], id)
		if len(a.active[p]) == 0 {
			delete(a.active, p)
		}
		a.activeMu.Unlock()
	}, nil
}

func accountLoginError(p *streamPermit) error {
	if p.Grant != "" {
		return ErrSharedNeedsLogin
	}
	return ErrNeedsLogin
}
