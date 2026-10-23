package gateway

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
	"github.com/my-name-is-alan/yimeng/core/internal/online"
	"github.com/my-name-is-alan/yimeng/core/internal/vault"
)

// Every factory call creates a distinct plugin/client; unlike the legacy fake,
// this exercises the same per-account instance boundary as real platforms.
type identityPlugin struct {
	*gwPlugin
	feedback *sync.Map
}

func (p *identityPlugin) RecommendTracks(ctx context.Context) ([]online.Track, error) {
	if p.env.HTTP.Cookie("S") == "" {
		return nil, online.ErrNeedsLogin
	}
	return []online.Track{gwTrack("full", "daily-"+p.env.HTTP.Cookie("U"), online.EntPreview)}, nil
}
func (p *identityPlugin) UserPlaylists(ctx context.Context) ([]online.Playlist, error) {
	u := p.env.HTTP.Cookie("U")
	if u == "" {
		return nil, online.ErrNeedsLogin
	}
	return []online.Playlist{{ID: "gw:private-" + u, Origin: "gw", Name: "mine-" + u, Kind: "liked", Mine: true}}, nil
}
func (p *identityPlugin) Playlist(ctx context.Context, id string) (online.Playlist, error) {
	if strings.HasPrefix(id, "private-") && id != "private-"+p.env.HTTP.Cookie("U") {
		return online.Playlist{}, online.ErrUnavailable
	}
	return online.Playlist{ID: "gw:" + id, Name: id, Tracks: []online.Track{gwTrack("full", "song-"+p.env.HTTP.Cookie("U"), online.EntPreview)}}, nil
}
func (p *identityPlugin) FM(ctx context.Context) ([]online.Track, error) {
	return p.RecommendTracks(ctx)
}
func (p *identityPlugin) FMFeedback(ctx context.Context, id, action string) error {
	p.feedback.Store(p.env.HTTP.Cookie("U"), id+":"+action)
	return nil
}
func (p *identityPlugin) Resolve(ctx context.Context, id string, q online.Quality) (online.Stream, error) {
	u := p.env.HTTP.Cookie("U")
	if u == "" {
		return online.Stream{}, online.ErrNeedsLogin
	}
	return online.Stream{URL: p.cdn + "/" + id, Header: map[string]string{"X-Account": u}, Format: "mp3", Entitlement: online.EntFull, ExpiresAt: time.Now().Add(time.Hour)}, nil
}

type isolationEnv struct {
	e                                          *env
	db                                         *sql.DB
	mgr                                        *online.Accounts
	admin, kid, other, adminID, kidID, otherID string
	feedback                                   *sync.Map
}

func newIsolationEnv(t *testing.T) *isolationEnv {
	t.Helper()
	x := &isolationEnv{feedback: &sync.Map{}}
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		if r.URL.Path == "/slow" {
			w.WriteHeader(200)
			w.Write([]byte("AUDIO-" + r.Header.Get("X-Account") + strings.Repeat("x", 8192)))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		http.ServeContent(w, r, "song.mp3", time.Time{}, bytes.NewReader([]byte("AUDIO-"+r.Header.Get("X-Account")+strings.Repeat("x", 512))))
	}))
	t.Cleanup(cdn.Close)
	x.e = newEnvWith(t, func(d *Deps, db *sql.DB, sc *library.Scanner) {
		x.db = db
		v, err := vault.NewSQLStore(db, make([]byte, 32))
		if err != nil {
			t.Fatal(err)
		}
		r := online.NewRegistry(db, v, d.Log, nil)
		if err := r.Register(func(env online.Env) (online.Plugin, error) {
			return &identityPlugin{&gwPlugin{env: env, cdn: cdn.URL, userFromCookie: true}, x.feedback}, nil
		}); err != nil {
			t.Fatal(err)
		}
		if err := r.Load(context.Background()); err != nil {
			t.Fatal(err)
		}
		d.Online = online.NewService(r, sc, online.NewCache(db), d.Log)
		d.OnlineLib = sc
		x.mgr, err = online.NewAccounts(context.Background(), d.Online)
		if err != nil {
			t.Fatal(err)
		}
		d.Accounts = x.mgr
		d.OnlinePublic, err = x.mgr.Public(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		sc.SetExternalResolver(x.mgr.Materialize)
	})
	_, b := x.e.do("POST", "/api/auth/setup", `{"username":"owner","password":"Str0ng-Isolation-42"}`, "")
	var session struct {
		Token string
		User  struct{ ID string }
	}
	if err := json.Unmarshal(b, &session); err != nil {
		t.Fatal(err)
	}
	x.admin, x.adminID = session.Token, session.User.ID
	if x.admin == "" {
		t.Fatalf("setup: %s", b)
	}
	for _, name := range []string{"child", "other"} {
		r, b := x.e.do("POST", "/api/users", fmt.Sprintf(`{"username":%q,"password":"Str0ng-Isolation-42","role":"user"}`, name), x.admin)
		if r.StatusCode != 201 {
			t.Fatal(string(b))
		}
		r, b = x.e.do("POST", "/api/auth/login", fmt.Sprintf(`{"username":%q,"password":"Str0ng-Isolation-42"}`, name), "")
		if r.StatusCode != 200 {
			t.Fatal(string(b))
		}
		if err := json.Unmarshal(b, &session); err != nil {
			t.Fatal(err)
		}
		if name == "child" {
			x.kid, x.kidID = session.Token, session.User.ID
		} else {
			x.other, x.otherID = session.Token, session.User.ID
		}
	}
	r, b := x.e.do("PUT", "/api/plugins/gw", `{"enabled":true}`, x.admin)
	if r.StatusCode != 200 {
		t.Fatal(string(b))
	}
	return x
}
func (x *isolationEnv) login(token, account string) {
	x.e.t.Helper()
	r, b := x.e.do("POST", "/api/plugins/gw/login", fmt.Sprintf(`{"method":"cookie","cookie":"S=secret-%s; U=%s"}`, account, account), token)
	if r.StatusCode != 200 {
		x.e.t.Fatalf("platform login: %d %s", r.StatusCode, b)
	}
}
func (x *isolationEnv) grant(account string) string {
	x.e.t.Helper()
	r, b := x.e.do("POST", "/api/admin/stream-grants", fmt.Sprintf(`{"consumer_user_id":%q,"platform":"gw","account_id":%q,"label":"家庭播放"}`, x.kidID, account), x.admin)
	if r.StatusCode != 201 {
		x.e.t.Fatalf("grant: %d %s", r.StatusCode, b)
	}
	var g struct{ ID string }
	json.Unmarshal(b, &g)
	return g.ID
}

func TestAccountIsolationPersonalDataAndFixedStream(t *testing.T) {
	x := newIsolationEnv(t)
	x.login(x.admin, "donor")
	x.login(x.kid, "child")
	x.login(x.other, "other")
	for _, who := range []struct{ token, account string }{{x.admin, "donor"}, {x.kid, "child"}, {x.other, "other"}} {
		for attempt := 0; attempt < 2; attempt++ {
			for _, path := range []string{"/api/online/gw/daily", "/api/online/gw/user/playlists", "/api/plugins/gw", "/api/server/info"} {
				r, b := x.e.do("GET", path, "", who.token)
				if r.StatusCode != 200 || !strings.Contains(string(b), who.account) {
					t.Fatalf("%s %s: %d %s", who.account, path, r.StatusCode, b)
				}
				for _, foreign := range []string{"donor", "child", "other"} {
					if foreign != who.account && strings.Contains(string(b), foreign) {
						t.Fatalf("cross-user data: %s", b)
					}
				}
			}
		}
	}
	// Even a real personal login is not a fallback source for a member.
	if r, b := x.e.do("GET", "/api/online/resolve/gw:full", "", x.kid); r.StatusCode != 403 || !strings.Contains(string(b), "shared_stream_required") {
		t.Fatalf("ungranted: %d %s", r.StatusCode, b)
	}
	g := x.grant("donor")
	// Changing the administrator's personal default does not change the grant.
	x.login(x.admin, "second")
	for _, path := range []string{"/api/stream/gw:full", "/api/tracks/gw:full/download"} {
		r, b := x.e.do("GET", path, "", x.kid)
		if r.StatusCode != 200 || !strings.HasPrefix(string(b), "AUDIO-donor") {
			t.Fatalf("fixed donor: %d %s", r.StatusCode, b)
		}
	}
	r, b := x.e.do("GET", "/api/stream/gw:full", "", x.kid, "Range", "bytes=0-10")
	if r.StatusCode != 206 || !strings.HasPrefix(string(b), "AUDIO-donor") {
		t.Fatalf("range: %d %s", r.StatusCode, b)
	}
	if r, b := x.e.do("GET", "/api/plugins/gw/accounts", "", x.kid); r.StatusCode != 200 || strings.Contains(string(b), "donor") {
		t.Fatalf("account list leak: %s", b)
	}
	if r, _ := x.e.do("POST", "/api/plugins/gw/accounts/donor/default", "", x.kid); r.StatusCode != 404 {
		t.Fatalf("cross-user switch: %d", r.StatusCode)
	}
	if r, _ := x.e.do("GET", "/api/online/gw/playlists/private-donor", "", x.kid); r.StatusCode != 404 {
		t.Fatalf("private playlist: %d", r.StatusCode)
	}
	if r, b := x.e.do("POST", "/api/online/gw/fm/feedback", `{"track_id":"gw:full","action":"like"}`, x.kid); r.StatusCode != 204 && r.StatusCode != 200 {
		t.Fatalf("feedback: %d %s", r.StatusCode, b)
	}
	if _, ok := x.feedback.Load("child"); !ok {
		t.Fatal("feedback not sent to personal account")
	}
	if _, ok := x.feedback.Load("donor"); ok {
		t.Fatal("feedback polluted donor")
	}
	if r, _ := x.e.do("POST", "/api/admin/stream-grants", `{}`, x.kid); r.StatusCode != 403 {
		t.Fatal("member created grant")
	}
	if r, _ := x.e.do("DELETE", "/api/admin/stream-grants/"+g, "", x.admin); r.StatusCode != 204 {
		t.Fatal("revoke failed")
	}
	for _, path := range []string{"/api/online/resolve/gw:full", "/api/stream/gw:full", "/api/tracks/gw:full/download"} {
		if r, b := x.e.do("GET", path, "", x.kid); r.StatusCode != 403 || !strings.Contains(string(b), "shared_stream_revoked") {
			t.Fatalf("revoked bypass: %s %d %s", path, r.StatusCode, b)
		}
	}
	if r, _ := x.e.do("HEAD", "/api/stream/gw:full", "", x.kid); r.StatusCode != 403 {
		t.Fatalf("HEAD revoked: %d", r.StatusCode)
	}
	// Core history/favorites still belong to the listener, not the donor.
	x.grant("donor")
	if r, b := x.e.do("POST", "/api/scrobble", `{"track_id":"gw:full","duration_ms":180000,"completed":true}`, x.kid); r.StatusCode != 204 && r.StatusCode != 200 {
		t.Fatalf("history: %d %s", r.StatusCode, b)
	}
	_, mine := x.e.do("GET", "/api/history", "", x.kid)
	_, admin := x.e.do("GET", "/api/history", "", x.admin)
	if !strings.Contains(string(mine), "gw:full") || strings.Contains(string(admin), "gw:full") {
		t.Fatalf("history ownership: mine=%s admin=%s", mine, admin)
	}
}

func TestAccountIsolationChallengeAndRevocation(t *testing.T) {
	x := newIsolationEnv(t)
	x.login(x.admin, "donor")
	if r, b := x.e.do("GET", "/api/home", "", x.kid); r.StatusCode != 200 || strings.Contains(string(b), "daily-donor") {
		t.Fatalf("anonymous personalization: %s", b)
	}
	if r, _ := x.e.do("POST", "/api/plugins/gw/login/qr", "", x.admin); r.StatusCode != 200 {
		t.Fatal("start qr")
	}
	if r, _ := x.e.do("GET", "/api/plugins/gw/login/qr/k1", "", x.kid); r.StatusCode != 404 {
		t.Fatalf("cross-user QR poll: %d", r.StatusCode)
	}
	g := x.grant("donor")
	req, _ := http.NewRequest("GET", x.e.srv.URL+"/api/stream/gw:slow", nil)
	req.Header.Set("Authorization", "Bearer "+x.kid)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	buf := make([]byte, len("AUDIO-donor"))
	if _, err := io.ReadFull(res.Body, buf); err != nil {
		t.Fatal(err)
	}
	if r, b := x.e.do("DELETE", "/api/admin/stream-grants/"+g, "", x.admin); r.StatusCode != 204 {
		t.Fatalf("revoke %s", b)
	}
	done := make(chan struct{})
	go func() { io.Copy(io.Discard, res.Body); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("revoked stream still active")
	}
	// Subsonic tokens and old query-token URLs must hit the same authorization.
	q := url.Values{"u": {"child"}, "p": {x.kid}, "v": {"1.16.1"}, "c": {"test"}, "id": {"gw:full"}}
	if r, b := x.e.do("GET", "/rest/stream.view?"+q.Encode(), "", ""); r.StatusCode != 403 {
		t.Fatalf("Subsonic bypass: %d %s", r.StatusCode, b)
	}
	if r, _ := x.e.do("GET", "/api/stream/gw:full?access_token="+url.QueryEscape(x.kid), "", ""); r.StatusCode != 403 {
		t.Fatal("query-token bypass")
	}
	x.e.auth.SetSigningKey([]byte("offline-isolation-test-signing-key"))
	dev, err := x.e.auth.Authenticate(context.Background(), x.kid)
	if err != nil {
		t.Fatal(err)
	}
	signed := x.e.auth.SignURL("/api/stream/gw:full", dev.ID, time.Hour)
	if r, _ := x.e.do("GET", signed, "", ""); r.StatusCode != 403 {
		t.Fatalf("signed URL bypass: %d", r.StatusCode)
	}
	key := x.e.mkCred(x.admin, `{"kind":"api_key","name":"isolation-test","scopes":["read","playback","admin"]}`)
	for _, path := range []string{"/api/plugins/gw/login", "/api/admin/stream-grants", "/api/admin/platform-legacy/claim"} {
		if r, _ := x.e.do("POST", path, `{}`, key.Secret); r.StatusCode != 403 {
			t.Fatalf("API key changed account security: %s %d", path, r.StatusCode)
		}
	}
}

func TestAccountIsolationConcurrentAndDeletion(t *testing.T) {
	x := newIsolationEnv(t)
	x.login(x.admin, "donor")
	x.login(x.kid, "child")
	x.login(x.other, "other")
	x.grant("donor")
	var wg sync.WaitGroup
	for _, who := range []struct{ token, account string }{{x.kid, "child"}, {x.other, "other"}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 8; i++ {
				r, b := x.e.do("GET", "/api/online/gw/daily", "", who.token)
				if r.StatusCode != 200 || !strings.Contains(string(b), "daily-"+who.account) {
					t.Errorf("mixed concurrent responses: %s", b)
				}
			}
		}()
	}
	wg.Wait()
	if r, b := x.e.do("DELETE", "/api/users/"+x.kidID, "", x.admin); r.StatusCode != 204 {
		t.Fatalf("delete: %s", b)
	}
	for _, table := range []string{"online_owner_state", "online_secret_refs"} {
		var n int
		if err := x.db.QueryRow("SELECT count(*) FROM "+table+" WHERE user_id=?", x.kidID).Scan(&n); err != nil || n != 0 {
			t.Fatalf("orphan %s: %d %v", table, n, err)
		}
	}
	var grants int
	x.db.QueryRow(`SELECT count(*) FROM stream_grants WHERE consumer_user_id=?`, x.kidID).Scan(&grants)
	if grants != 0 {
		t.Fatal("orphan grant")
	}
}
