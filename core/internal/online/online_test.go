package online

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/infra/db"
	"github.com/my-name-is-alan/yimeng/core/internal/library"
	"github.com/my-name-is-alan/yimeng/core/internal/vault"
	"github.com/my-name-is-alan/yimeng/core/migrations"
)

// fakePlugin is an in-memory platform: tracks by native id with fixed
// entitlements. It never touches the network.
type fakePlugin struct {
	id     string
	env    Env
	tracks map[string]Track
	ent    map[string]Entitlement
	mu     sync.Mutex
	calls  int
}

func (f *fakePlugin) Manifest() Manifest {
	return Manifest{ID: f.id, Name: f.id, Version: "t", Qualities: []Quality{QStandard, QHigh, QLossless},
		LoginMethods: []LoginMethod{LoginCookie}, Capabilities: []Capability{CapSearch, CapStream},
		Config: []ConfigField{{Key: "region", Label: "r", Type: "select", Options: []ConfigOption{{Value: "cn"}, {Value: "hk"}}, Default: "cn"}}}
}
func (f *fakePlugin) Search(ctx context.Context, q string, limit int) (SearchResult, error) {
	var r SearchResult
	for _, t := range f.tracks {
		if strings.Contains(q, t.Title) {
			r.Tracks = append(r.Tracks, t)
		}
	}
	return r, nil
}
func (f *fakePlugin) Tracks(ctx context.Context, ids []string) ([]Track, error) {
	var out []Track
	for _, id := range ids {
		if t, ok := f.tracks[id]; ok {
			out = append(out, t)
		}
	}
	return out, nil
}
func (f *fakePlugin) Album(context.Context, string) (Album, error) { return Album{}, ErrNotSupported }
func (f *fakePlugin) Artist(context.Context, string) (Artist, error) {
	return Artist{}, ErrNotSupported
}
func (f *fakePlugin) Playlist(context.Context, string) (Playlist, error) {
	return Playlist{}, ErrNotSupported
}
func (f *fakePlugin) Lyrics(context.Context, string) (Lyrics, error) {
	return Lyrics{}, ErrNotSupported
}
func (f *fakePlugin) Resolve(ctx context.Context, id string, q Quality) (Stream, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	e, ok := f.ent[id]
	if !ok {
		return Stream{}, ErrNotSupported
	}
	s := Stream{Entitlement: e, Quality: q}
	if e != EntUnavailable {
		s.URL = "https://cdn.example/" + f.id + "/" + id
	}
	if e == EntPreview {
		s.PreviewStartMs, s.PreviewEndMs = 30000, 60000
	}
	return s, nil
}
func (f *fakePlugin) StartQR(context.Context) (QRChallenge, error) {
	return QRChallenge{}, ErrNotSupported
}
func (f *fakePlugin) PollQR(context.Context, string) (QRPoll, error) {
	return QRPoll{}, ErrNotSupported
}
func (f *fakePlugin) Account(context.Context) (Account, error) {
	return Account{UserID: "u", Nickname: "n"}, nil
}
func (f *fakePlugin) Refresh(context.Context) error { return nil }
func (f *fakePlugin) LoginPassword(context.Context, string, string) (Account, error) {
	return Account{}, ErrNotSupported
}
func (f *fakePlugin) LoginCookie(ctx context.Context, c string) (Account, error) {
	f.env.HTTP.ReplaceCookies(ParseCookieString(c))
	return Account{UserID: "u", Nickname: "n"}, nil
}

func factory(p *fakePlugin) Factory {
	return func(e Env) (Plugin, error) {
		p.env = e
		return p, nil
	}
}

type fakeLib struct {
	eqs   map[string][]library.Equivalent
	match map[string]string // title -> local id
	links []string
}

func (l *fakeLib) UpsertExternal(context.Context, []library.ExternalTrack) error { return nil }
func (l *fakeLib) FindMatch(ctx context.Context, q library.MatchQuery) (string, float64, string, error) {
	if id := l.match[q.Title]; id != "" {
		return id, 0.9, "title_artist_duration", nil
	}
	return "", 0, "", nil
}
func (l *fakeLib) LinkEquivalent(ctx context.Context, a, b string, c float64, m string) error {
	l.links = append(l.links, a+"="+b)
	return nil
}
func (l *fakeLib) Equivalents(ctx context.Context, id string) ([]library.Equivalent, error) {
	return l.eqs[id], nil
}

func setup(t *testing.T) (*Registry, *Service, *fakePlugin, *fakePlugin, *fakeLib, vault.Vault) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := db.Migrate(context.Background(), d, migrations.FS); err != nil {
		t.Fatal(err)
	}
	v, err := vault.NewSQLStore(d, make([]byte, vault.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	song := func(o, id, title string) Track {
		return Track{ID: o + ":" + id, Origin: o, Title: title, ArtistName: "歌手", DurationMs: 200000}
	}
	a := &fakePlugin{id: "fa", tracks: map[string]Track{"1": song("fa", "1", "晴天"), "2": song("fa", "2", "七里香"), "3": song("fa", "3", "稻香")},
		ent: map[string]Entitlement{"1": EntPreview, "2": EntPreview, "3": EntUnavailable}}
	b := &fakePlugin{id: "fb", tracks: map[string]Track{"9": song("fb", "9", "七里香")}, ent: map[string]Entitlement{"9": EntFull}}
	reg := NewRegistry(d, v, nil, http.DefaultClient)
	for _, p := range []*fakePlugin{a, b} {
		if err := reg.Register(factory(p)); err != nil {
			t.Fatal(err)
		}
	}
	if err := reg.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	lib := &fakeLib{eqs: map[string][]library.Equivalent{}, match: map[string]string{}}
	return reg, NewService(reg, lib, NewCache(d), nil), a, b, lib, v
}

func enable(t *testing.T, r *Registry, id string) {
	on := true
	if _, err := r.Configure(context.Background(), id, &on, nil); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryConfigAndVault(t *testing.T) {
	reg, _, _, _, _, v := setup(t)
	ctx := context.Background()
	st := reg.List(ctx, false)
	if len(st) != 2 || st[0].Enabled || st[0].Config[KeyQuality] != "high" || st[0].Config["region"] != "cn" {
		t.Fatalf("defaults %+v", st[0])
	}
	if _, err := reg.Configure(ctx, "fa", nil, map[string]any{"bogus": 1}); err == nil {
		t.Fatal("unknown key accepted")
	}
	if _, err := reg.Configure(ctx, "fa", nil, map[string]any{KeyRate: 1.0}); err == nil {
		t.Fatal("rate below min accepted")
	}
	if _, err := reg.Configure(ctx, "fa", nil, map[string]any{KeyQuality: "hires"}); err == nil {
		t.Fatal("unsupported quality accepted")
	}
	on := true
	s, err := reg.Configure(ctx, "fa", &on, map[string]any{KeyQuality: "lossless", KeyRate: "120"})
	if err != nil || !s.Enabled || s.Config[KeyQuality] != "lossless" || s.Config[KeyRate] != 120.0 {
		t.Fatalf("configure %+v %v", s, err)
	}
	if _, _, err := reg.Enabled("fb"); !errors.Is(err, ErrDisabled) {
		t.Fatal("fb should be disabled")
	}
	if _, err := reg.LoginCookie(ctx, "fa", "SESSION=abc"); err != nil {
		t.Fatal(err)
	}
	c, err := v.Get(ctx, "plugin.fa")
	if err != nil || c.Fields["SESSION"] != "abc" {
		t.Fatalf("vault %+v %v", c, err)
	}
	// Status never carries cookies
	if s, _ := reg.Status(ctx, "fa"); !s.LoggedIn || s.Account == nil {
		t.Fatalf("status %+v", s)
	}
	// a fresh registry over the same db restores settings and session
	reg2 := NewRegistry(reg.db, v, nil, http.DefaultClient)
	reg2.Register(factory(&fakePlugin{id: "fa"}))
	reg2.Load(ctx)
	if !reg2.LoggedIn("fa") || reg2.EnabledIDs()[0] != "fa" {
		t.Fatal("not restored")
	}
	if cl, _ := reg2.get("fa"); cl.client.Cookie("SESSION") != "abc" || cl.config[KeyQuality] != "lossless" {
		t.Fatal("jar/config not restored")
	}
	if err := reg.Logout(ctx, "fa"); err != nil || reg.LoggedIn("fa") {
		t.Fatal("logout")
	}
	if _, err := v.Get(ctx, "plugin.fa"); err == nil {
		t.Fatal("vault entry should be gone")
	}
}

func TestClientCooldown(t *testing.T) {
	c := NewClient("x", 60, http.DefaultClient)
	now := time.Unix(1000, 0)
	c.now = func() time.Time { return now }
	c.Penalize()
	if got := c.CooldownUntil().Sub(now); got != 30*time.Second {
		t.Fatalf("first strike %v", got)
	}
	c.Penalize()
	if got := c.CooldownUntil().Sub(now); got != time.Minute {
		t.Fatalf("second strike %v", got)
	}
	for i := 0; i < 10; i++ {
		c.Penalize()
	}
	if got := c.CooldownUntil().Sub(now); got != 15*time.Minute {
		t.Fatalf("cap %v", got)
	}
	req, _ := http.NewRequest("GET", "http://127.0.0.1:1/", nil)
	if _, err := c.Do(req); !errors.Is(err, ErrRiskControl) {
		t.Fatalf("cooldown must short-circuit: %v", err)
	}
	now = now.Add(16 * time.Minute)
	if !c.CooldownUntil().IsZero() {
		t.Fatal("cooldown should expire")
	}
	c.Reward()
	c.Penalize()
	if got := c.CooldownUntil().Sub(now); got != 30*time.Second {
		t.Fatalf("reward resets strikes %v", got)
	}
	m := ParseCookieString(" a=1; b = 2 ;bad; c=")
	if m["a"] != "1" || m["b"] != "2" || len(m) != 2 {
		t.Fatalf("parse %v", m)
	}
}

func TestResolveFallbackOrder(t *testing.T) {
	reg, svc, a, b, lib, _ := setup(t)
	ctx := context.Background()
	enable(t, reg, "fa")

	// preview only, nothing else available → original preview, never full
	r, err := svc.Resolve(ctx, "fa:1", "", false)
	if err != nil || r.Entitlement != EntPreview || r.PlayID != "fa:1" || r.Fallback != "" || r.PreviewEndMs != 60000 {
		t.Fatalf("preview %+v %v", r, err)
	}
	// lease is reused
	calls := a.calls
	svc.Resolve(ctx, "fa:1", "", false)
	if a.calls != calls {
		t.Fatal("lease not reused")
	}
	// local file match wins
	lib.match["晴天"] = "localabc"
	r, _ = svc.Resolve(ctx, "fa:1", "", true)
	if !r.Local || r.PlayID != "localabc" || r.Fallback != "local" || len(lib.links) != 1 {
		t.Fatalf("local %+v", r)
	}
	// other platform only if enabled AND logged in
	r, _ = svc.Resolve(ctx, "fa:2", "", true)
	if r.Fallback != "" || r.Entitlement != EntPreview {
		t.Fatalf("must not use a platform without login %+v", r)
	}
	enable(t, reg, "fb")
	r, _ = svc.Resolve(ctx, "fa:2", "", true)
	if r.Fallback != "" {
		t.Fatalf("fb not logged in %+v", r)
	}
	reg.LoginCookie(ctx, "fb", "S=1")
	r, err = svc.Resolve(ctx, "fa:2", "", true)
	if err != nil || r.Fallback != "fb" || r.PlayID != "fb:9" || r.Entitlement != EntFull {
		t.Fatalf("cross-account %+v %v", r, err)
	}
	if b.calls == 0 {
		t.Fatal("fb resolve not called")
	}
	// unavailable stays unavailable
	r, _ = svc.Resolve(ctx, "fa:3", "", true)
	if r.Entitlement != EntUnavailable || r.URL != "" {
		t.Fatalf("unavailable %+v", r)
	}
	// disabled origin errors
	off := false
	reg.Configure(ctx, "fa", &off, nil)
	if _, err := svc.Resolve(ctx, "fa:3", "", true); !errors.Is(err, ErrDisabled) {
		t.Fatalf("disabled %v", err)
	}
	// …but a known local copy still plays
	if r, _ := svc.Resolve(ctx, "fa:1", "", true); !r.Local {
		t.Fatal("local copy should play while plugin disabled")
	}
	// local ids pass through
	if r, _ := svc.Resolve(ctx, "abcdef", "", false); !r.Local || r.PlayID != "abcdef" {
		t.Fatalf("local passthrough %+v", r)
	}
}

func TestCacheTTL(t *testing.T) {
	_, svc, _, _, _, _ := setup(t)
	c := svc.Cache
	now := time.Unix(5000, 0)
	c.now = func() time.Time { return now }
	ctx := context.Background()
	c.Put(ctx, Key("fa", "track", "1"), map[string]int{"v": 1}, time.Minute)
	var got map[string]int
	if !c.Get(ctx, Key("fa", "track", "1"), &got) || got["v"] != 1 {
		t.Fatal("miss")
	}
	now = now.Add(2 * time.Minute)
	if c.Get(ctx, Key("fa", "track", "1"), &got) {
		t.Fatal("expired entry returned")
	}
	if n, _ := c.Prune(ctx); n != 1 {
		t.Fatalf("prune %d", n)
	}
}

func TestDeviceIdentityPersistence(t *testing.T) {
	reg, _, _, _, _, v := setup(t)
	ctx := context.Background()
	in, _ := reg.get("fa")
	// anonymous: only device.* keys reach the Vault, and they don't count as login
	in.client.SetCookie(DevicePrefix+"mid", "123")
	in.client.SetCookie("tracking", "x")
	reg.Persist(ctx, "fa")
	c, err := v.Get(ctx, "plugin.fa")
	if err != nil || c.Fields[DevicePrefix+"mid"] != "123" || c.Fields["tracking"] != "" {
		t.Fatalf("anonymous persist %+v %v", c, err)
	}
	reg2 := NewRegistry(reg.db, v, nil, http.DefaultClient)
	reg2.Register(factory(&fakePlugin{id: "fa"}))
	reg2.Load(ctx)
	if reg2.LoggedIn("fa") {
		t.Fatal("device ids must not count as a session")
	}
	if in2, _ := reg2.get("fa"); in2.client.Cookie(DevicePrefix+"mid") != "123" {
		t.Fatal("device id not restored")
	}
	// login then logout keeps the device identity
	if _, err := reg.LoginCookie(ctx, "fa", "SESSION=abc"); err != nil {
		t.Fatal(err)
	}
	in.client.SetCookie(DevicePrefix+"mid", "123")
	reg.Persist(ctx, "fa")
	if err := reg.Logout(ctx, "fa"); err != nil {
		t.Fatal(err)
	}
	c, err = v.Get(ctx, "plugin.fa")
	if err != nil || c.Fields["SESSION"] != "" || c.Fields[DevicePrefix+"mid"] != "123" {
		t.Fatalf("after logout %+v %v", c, err)
	}
}
