package online

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/owner"
	"github.com/my-name-is-alan/yimeng/core/internal/vault"
)

type tenantPlugin struct{ fakePlugin }

func (p *tenantPlugin) LoginCookie(ctx context.Context, c string) (Account, error) {
	p.env.HTTP.ReplaceCookies(map[string]string{"sid": c, "U": c})
	return p.Account(ctx)
}
func (p *tenantPlugin) Account(context.Context) (Account, error) {
	u := p.env.HTTP.Cookie("U")
	if u == "" {
		return Account{}, ErrNeedsLogin
	}
	return Account{UserID: u, Nickname: u}, nil
}
func (p *tenantPlugin) Resolve(ctx context.Context, id string, q Quality) (Stream, error) {
	u := p.env.HTTP.Cookie("U")
	if u == "" {
		return Stream{}, ErrNeedsLogin
	}
	return Stream{URL: "https://audio.example/" + u, Entitlement: EntFull, ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func tenantBase(t *testing.T, admins int) (*Registry, *Service) {
	t.Helper()
	old, _, _, _, lib, v := setup(t)
	for _, u := range []struct{ id, role string }{{"admin", "admin"}, {"child", "user"}, {"other", "user"}} {
		if _, err := old.db.Exec(`INSERT INTO users(id,username,pw_hash,role,created_at,updated_at) VALUES(?,?, 'unused',?,1,1)`, u.id, u.id, u.role); err != nil {
			t.Fatal(err)
		}
	}
	if admins > 1 {
		if _, err := old.db.Exec(`INSERT INTO users(id,username,pw_hash,role,created_at,updated_at) VALUES('admin2','admin2','unused','admin',2,2)`); err != nil {
			t.Fatal(err)
		}
	}
	r := NewRegistry(old.db, v, nil, nil)
	if err := r.Register(func(env Env) (Plugin, error) {
		return &tenantPlugin{fakePlugin{id: "tenant", env: env, tracks: map[string]Track{}, ent: map[string]Entitlement{}}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	enable(t, r, "tenant")
	return r, NewService(r, lib, NewCache(r.db), nil)
}
func acquired(t *testing.T, a *Accounts, uid string, write bool) (context.Context, *Service, func()) {
	t.Helper()
	ctx, release, err := a.Acquire(owner.With(context.Background(), uid, uid == "admin" || uid == "admin2"), write)
	if err != nil {
		t.Fatal(err)
	}
	return ctx, FromContext(ctx), release
}

func TestPersonalMigrationAndRestart(t *testing.T) {
	r, base := tenantBase(t, 1)
	ctx := context.Background()
	if _, err := r.LoginCookie(ctx, "tenant", "legacy"); err != nil {
		t.Fatal(err)
	}
	a, err := NewAccounts(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.vault.Get(ctx, vaultKey("tenant")); !errors.Is(err, vault.ErrNotFound) {
		t.Fatalf("legacy active secret not removed: %v", err)
	}
	c, admin, done := acquired(t, a, "admin", false)
	if !admin.Reg.LoggedIn("tenant") {
		t.Fatal("administrator session not migrated")
	}
	accounts, err := admin.Reg.Accounts(c, "tenant")
	done()
	if err != nil || len(accounts) != 1 || accounts[0].ID != "legacy" {
		t.Fatalf("migration accounts: %+v %v", accounts, err)
	}
	_, child, done := acquired(t, a, "child", false)
	if child.Reg.LoggedIn("tenant") {
		t.Fatal("legacy session shared with child")
	}
	done()
	c, admin, done = acquired(t, a, "admin", true)
	_, err = admin.Reg.LoginCookie(c, "tenant", "new-personal")
	done()
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := NewAccounts(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	c, admin, done = acquired(t, restarted, "admin", false)
	st, err := admin.Reg.Status(c, "tenant")
	done()
	if err != nil || st.Account == nil || st.Account.UserID != "new-personal" || len(st.Accounts) != 2 {
		t.Fatalf("restart overwritten by legacy: %+v %v", st, err)
	}
	var grants int
	r.db.QueryRow(`SELECT count(*) FROM stream_grants`).Scan(&grants)
	if grants != 0 {
		t.Fatal("migration silently enabled sharing")
	}
}

func TestAmbiguousMigrationRequiresClaim(t *testing.T) {
	r, base := tenantBase(t, 2)
	ctx := context.Background()
	r.LoginCookie(ctx, "tenant", "old-owner")
	a, err := NewAccounts(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	if !a.LegacyPending(ctx) {
		t.Fatal("ambiguous owner was guessed")
	}
	for _, uid := range []string{"admin", "admin2", "child"} {
		_, s, done := acquired(t, a, uid, false)
		if s.Reg.LoggedIn("tenant") {
			t.Errorf("quarantined session exposed to %s", uid)
		}
		done()
	}
	if err := a.MigrateLegacy(ctx, "child"); !errors.Is(err, ErrIdentityRequired) {
		t.Fatal("child claimed legacy account")
	}
	if err := a.MigrateLegacy(ctx, "admin2"); err != nil {
		t.Fatal(err)
	}
	if err := a.ConfigChanged(); err != nil {
		t.Fatal(err)
	}
	_, s, done := acquired(t, a, "admin2", false)
	if !s.Reg.LoggedIn("tenant") {
		t.Fatal("claim not loaded")
	}
	done()
	_, s, done = acquired(t, a, "admin", false)
	if s.Reg.LoggedIn("tenant") {
		t.Fatal("claim leaked to other administrator")
	}
	done()
}

func TestLegacySessionWithoutAccountList(t *testing.T) {
	r, base := tenantBase(t, 1)
	ctx := context.Background()
	// Pre-multi-account installs only had the active credential blob.
	if err := r.vault.Put(ctx, vaultKey("tenant"), vault.Credential{Method: vault.LoginCookie, Fields: map[string]string{"sid": "old", "U": "old"}}); err != nil {
		t.Fatal(err)
	}
	a, err := NewAccounts(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	c, s, done := acquired(t, a, "admin", false)
	defer done()
	st, err := s.Reg.Status(c, "tenant")
	if err != nil || !st.LoggedIn || st.ActiveAccount != "legacy" || st.Account == nil || st.Account.UserID != "old" {
		t.Fatalf("active-only legacy session lost: %+v %v", st, err)
	}
}

func TestPrivateCacheScopesAndStaleRefresh(t *testing.T) {
	_, base := tenantBase(t, 1)
	a, err := NewAccounts(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	_, s1, d1 := acquired(t, a, "child", false)
	c1 := s1.Cache
	d1()
	_, s2, d2 := acquired(t, a, "other", false)
	c2 := s2.Cache
	d2()
	key := Key("tenant", "playlist", "private-1")
	ctx := context.Background()
	if err := c1.Put(ctx, key, "child-private", time.Hour); err != nil {
		t.Fatal(err)
	}
	var got string
	if c2.Get(ctx, key, &got) {
		t.Fatal("private cache crossed owner boundary")
	}
	if err := c2.Put(ctx, key, "other-private", time.Hour); err != nil {
		t.Fatal(err)
	}
	// Only one scope is expired; stale refreshes and coalescing remain scoped.
	if _, err := a.db.Exec(`UPDATE external_cache SET expires_at=0 WHERE key=?`, c1.storageKey(key)); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	value, err := cached(ctx, c1, key, time.Hour, func(context.Context) (string, error) { calls.Add(1); return "child-refreshed", nil })
	if err != nil || value != "child-private" {
		t.Fatalf("stale result: %q %v", value, err)
	}
	c1.Wait()
	if calls.Load() != 1 || !c2.Get(ctx, key, &got) || got != "other-private" {
		t.Fatal("SWR crossed cache namespace")
	}
	// Login/account changes use a new persisted cache generation.
	_, changed, done := acquired(t, a, "child", true)
	defer done()
	if changed.Cache.Get(ctx, key, &got) {
		t.Fatal("old identity's cache survived generation change")
	}
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := cached(ctx, changed.Cache, key, time.Hour, func(context.Context) (string, error) { time.Sleep(20 * time.Millisecond); return "new-identity", nil })
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}

func TestSharedPermitSurvivesUnrelatedSwitchButNotAccountRemoval(t *testing.T) {
	_, base := tenantBase(t, 1)
	a, err := NewAccounts(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	c, s, done := acquired(t, a, "admin", true)
	_, err = s.Reg.LoginCookie(c, "tenant", "donor")
	done()
	if err != nil {
		t.Fatal(err)
	}
	adminCtx := owner.With(context.Background(), "admin", true)
	kidCtx := owner.With(context.Background(), "child", false)
	g, err := a.Grant(adminCtx, "child", "tenant", "donor", "family")
	if err != nil {
		t.Fatal(err)
	}
	r, err := a.Resolve(kidCtx, "tenant:1", QHigh, false)
	if err != nil {
		t.Fatal(err)
	}
	live, closeStream, err := a.Guard(kidCtx, r)
	if err != nil {
		t.Fatal(err)
	}
	defer closeStream()
	c, s, done = acquired(t, a, "admin", true)
	_, err = s.Reg.LoginCookie(c, "tenant", "other-default")
	a.SessionsChanged(c, "admin")
	done()
	if err != nil {
		t.Fatal(err)
	}
	if live.Err() != nil {
		t.Fatal("unrelated default switch cancelled pinned donor")
	}
	r, err = a.Resolve(kidCtx, "tenant:1", QHigh, false)
	if err != nil || r.URL != "https://audio.example/donor" {
		t.Fatalf("donor changed: %+v %v", r, err)
	}
	c, s, done = acquired(t, a, "admin", true)
	_, err = s.Reg.RemoveAccount(c, "tenant", "donor")
	a.SessionsChanged(c, "admin")
	done()
	if err != nil {
		t.Fatal(err)
	}
	if live.Err() == nil {
		t.Fatal("removed donor still streaming")
	}
	if _, err = a.Resolve(kidCtx, "tenant:1", QHigh, false); err == nil {
		t.Fatal("old lease bypassed account removal")
	}
	if err := a.Revoke(owner.With(context.Background(), "other", false), g.ID); !errors.Is(err, ErrIdentityRequired) {
		t.Fatal("non-admin revoked grant")
	}
}
