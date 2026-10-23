package netease

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/my-name-is-alan/yimeng/core/internal/online"
)

func TestEapiVector(t *testing.T) {
	got := eapiEncrypt("/api/song/lyric", []byte(`{"id":"1"}`))
	want := "9B1595479B79266065903BA79E9883029338432CD7D98D1A3626E997A2C53121AC2B599B578814D951DEFA214A4737E544B39A11DA780080374501DFDC0A5283F555AFF100F5F9A67BC1AE911F2217ACEBF3DCD872EE4323CA6AF88A5A9AD142"
	if got != want {
		t.Fatalf("eapi mismatch\n got %s\nwant %s", got, want)
	}
	uri, text, err := eapiDecryptParams(got)
	if err != nil || uri != "/api/song/lyric" || string(text) != `{"id":"1"}` {
		t.Fatalf("roundtrip %q %q %v", uri, text, err)
	}
}

func TestWeapiVector(t *testing.T) {
	params, key := weapiWithKey([]byte(`{"a":1}`), "abcdefghijklmnop")
	if params != "Ho/4Pu1bYyrzwcnXPnTcXm1Gb04eSzwGSNb4NI3YqZY=" {
		t.Fatalf("params %s", params)
	}
	if key != "d15a1683c992095d0c234c19966605c5c5964911268bbeda8cb8d08d834913e59d53b32358903a121b5fca784c1f5ae44951fd02524df58ecc98e52cc7cf8689b42c2e93ddf05b0592512d87f5960467e2f086c018849d76014d323500e30f13ef4cafbb0cf5a66731a3f1776c75ca35d0062dac70a3e33245afabcf47938487" {
		t.Fatalf("encSecKey %s", key)
	}
	// both AES layers decrypt back
	outer, _ := base64.StdEncoding.DecodeString(params)
	mid, err := aesCBCDecrypt(outer, "abcdefghijklmnop", weapiIV)
	if err != nil {
		t.Fatal(err)
	}
	inner, _ := base64.StdEncoding.DecodeString(string(mid))
	plain, err := aesCBCDecrypt(inner, weapiPresetKey, weapiIV)
	if err != nil || string(plain) != `{"a":1}` {
		t.Fatalf("plain %q %v", plain, err)
	}
	if len(randomKey()) != 16 {
		t.Fatal("random key length")
	}
}

// fake is an offline stand-in for the NetEase hosts. eapi requests are
// decrypted to route by URI and inspect parameters; weapi routes by path.
type fake struct {
	mu       sync.Mutex
	routes   map[string]string // uri -> fixture file
	calls    []string
	eapiData map[string]map[string]any
	cookies  []string
}

func newFake() *fake {
	return &fake{eapiData: map[string]map[string]any{}, routes: map[string]string{
		"/api/login/qrcode/unikey":             "qr_unikey.json",
		"/api/login/qrcode/client/login":       "qr_waiting.json",
		"/api/w/nuser/account/get":             "account.json",
		"/api/v3/song/detail":                  "song_detail.json",
		"/api/song/enhance/player/url/v1":      "url_full.json",
		"/api/v6/playlist/detail":              "playlist_detail.json",
		"/api/toplist":                         "toplist.json",
		"/api/song/lyric/v1":                   "lyric.json",
		"/api/v3/discovery/recommend/songs":    "recommend_songs.json",
		"/api/v1/discovery/recommend/resource": "recommend_resource.json",
		"/api/personalized/playlist":           "personalized.json",
		"/api/user/playlist":                   "user_playlist.json",
		"/api/playlist/list":                   "top_playlists.json",
		"/api/v1/album/18905":                  "album.json",
		"/api/v1/artist/6452":                  "artist.json",
		"/api/w/login/cellphone":               "login_wrong.json",
		"/api/w/login":                         "login_wrong.json",
		"/api/login/token/refresh":             "login_ok.json",
		"/api/search/hot":                       "search_hot_detail.json",
	}}
}

func (f *fake) set(uri, file string) { f.mu.Lock(); f.routes[uri] = file; f.mu.Unlock() }

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	var uri string
	switch {
	case strings.HasPrefix(r.URL.Path, "/eapi/"):
		u, text, err := eapiDecryptParams(r.PostForm.Get("params"))
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		uri = u
		var m map[string]any
		json.Unmarshal(text, &m)
		f.mu.Lock()
		f.eapiData[uri] = m
		f.mu.Unlock()
	case strings.HasPrefix(r.URL.Path, "/weapi/"):
		if r.PostForm.Get("params") == "" || len(r.PostForm.Get("encSecKey")) != 256 {
			http.Error(w, "bad weapi", 400)
			return
		}
		uri = "/api/" + strings.TrimPrefix(r.URL.Path, "/weapi/")
	default:
		http.NotFound(w, r)
		return
	}
	f.mu.Lock()
	f.calls = append(f.calls, uri)
	f.cookies = append(f.cookies, r.Header.Get("Cookie"))
	file := f.routes[uri]
	f.mu.Unlock()
	// cloudsearch type switch
	if uri == "/api/cloudsearch/pc" {
		f.mu.Lock()
		typ := f.eapiData[uri]["type"]
		f.mu.Unlock()
		file = map[float64]string{1: "search_songs.json", 10: "search_albums.json", 100: "search_artists.json", 1000: "search_playlists.json"}[typ.(float64)]
	}
	if file == "" {
		http.NotFound(w, r)
		return
	}
	if file == "qr_ok.json" {
		http.SetCookie(w, &http.Cookie{Name: "MUSIC_U", Value: "secret-session", Path: "/"})
		http.SetCookie(w, &http.Cookie{Name: "__csrf", Value: "csrf1", Path: "/"})
	}
	b, err := os.ReadFile("testdata/" + file)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}

func newPlugin(t *testing.T) (*Plugin, *fake, *online.Client) {
	t.Helper()
	f := newFake()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c := online.NewClient("netease", 6000, srv.Client())
	p, err := New(Options{Domain: srv.URL, EapiDomain: srv.URL})(online.Env{HTTP: c, Config: online.Config{}})
	if err != nil {
		t.Fatal(err)
	}
	return p.(*Plugin), f, c
}

func TestQRLoginFlow(t *testing.T) {
	p, f, c := newPlugin(t)
	ctx := context.Background()
	ch, err := p.StartQR(ctx)
	if err != nil || ch.Key != "key-123" || !strings.Contains(ch.Content, "codekey=key-123") {
		t.Fatalf("start %+v %v", ch, err)
	}
	if st, _ := p.PollQR(ctx, "key-123"); st.Status != online.QRWaiting {
		t.Fatalf("want waiting %+v", st)
	}
	f.set("/api/login/qrcode/client/login", "qr_scanned.json")
	if st, _ := p.PollQR(ctx, "key-123"); st.Status != online.QRScanned || st.Nickname != "小明" {
		t.Fatalf("want scanned %+v", st)
	}
	f.set("/api/login/qrcode/client/login", "qr_ok.json")
	st, err := p.PollQR(ctx, "key-123")
	if err != nil || st.Status != online.QRConfirmed || st.Account == nil || st.Account.Nickname != "小明" || st.Account.VIP {
		t.Fatalf("confirm %+v %v", st, err)
	}
	if c.Cookie("MUSIC_U") != "secret-session" || !c.TakeDirty() {
		t.Fatal("session cookie not stored in jar")
	}
	if f.eapiData["/api/login/qrcode/client/login"]["key"] != "key-123" {
		t.Fatalf("poll params %v", f.eapiData["/api/login/qrcode/client/login"])
	}
	// later eapi calls carry MUSIC_U in the header cookie
	p.Lyrics(ctx, "186016")
	if last := f.cookies[len(f.cookies)-1]; !strings.Contains(last, "MUSIC_U=secret-session") {
		t.Fatalf("cookie header %q", last)
	}
}

func TestCookieAndPasswordLogin(t *testing.T) {
	p, f, c := newPlugin(t)
	ctx := context.Background()
	if _, err := p.LoginCookie(ctx, "foo=bar"); !errors.Is(err, online.ErrNeedsLogin) {
		t.Fatalf("missing MUSIC_U must fail: %v", err)
	}
	f.set("/api/w/nuser/account/get", "account_anon.json")
	if _, err := p.LoginCookie(ctx, "MUSIC_U=bad; __csrf=x"); !errors.Is(err, online.ErrNeedsLogin) || c.Cookie("MUSIC_U") == "bad" {
		t.Fatalf("invalid cookie must be rejected and rolled back: %v", err)
	}
	f.set("/api/w/nuser/account/get", "account.json")
	acc, err := p.LoginCookie(ctx, "MUSIC_U=good; __csrf=x; junk=1")
	if err != nil || acc.UserID != "1001" || c.Cookie("junk") != "" {
		t.Fatalf("cookie login %+v %v", acc, err)
	}
	if _, err := p.LoginPassword(ctx, "13800000000", "pw"); !errors.Is(err, online.ErrNeedsLogin) {
		t.Fatalf("wrong password: %v", err)
	}
	f.set("/api/w/login/cellphone", "risk.json")
	if _, err := p.LoginPassword(ctx, "13800000000", "pw"); !errors.Is(err, online.ErrRiskControl) {
		t.Fatalf("risk: %v", err)
	}
	f.set("/api/w/login", "login_ok.json")
	if acc, err := p.LoginPassword(ctx, "me@163.com", "pw"); err != nil || acc.Nickname != "小明" {
		t.Fatalf("email login %+v %v", acc, err)
	}
	if f.eapiData["/api/w/login"]["password"] != md5hex("pw") {
		t.Fatal("password must be sent as md5")
	}
}

func TestSearchAndEntitlement(t *testing.T) {
	p, _, _ := newPlugin(t)
	r, err := p.Search(context.Background(), "晴天", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Tracks) != 2 || len(r.Albums) != 1 || len(r.Artists) != 1 || len(r.Playlists) != 1 {
		t.Fatalf("counts %d %d %d %d", len(r.Tracks), len(r.Albums), len(r.Artists), len(r.Playlists))
	}
	t0 := r.Tracks[0]
	if t0.ID != "netease:186016" || t0.Playable != online.EntPreview || !t0.VIP || t0.Album.ID != "netease:18905" || t0.Year != 2003 || t0.MaxQuality != online.QLossless {
		t.Fatalf("vip track %+v", t0)
	}
	if r.Tracks[1].Playable != online.EntFull || r.Tracks[1].VIP {
		t.Fatalf("free track %+v", r.Tracks[1])
	}
	ts, err := p.Tracks(context.Background(), []string{"186016", "999", "5257138"})
	if err != nil || len(ts) != 3 {
		t.Fatal(err)
	}
	if ts[1].Playable != online.EntUnavailable {
		t.Fatalf("removed track must be unavailable: %+v", ts[1])
	}
}

func TestResolveEntitlement(t *testing.T) {
	p, f, _ := newPlugin(t)
	ctx := context.Background()
	s, err := p.Resolve(ctx, "5257138", online.QLossless)
	if err != nil || s.Entitlement != online.EntFull || s.Format != "flac" || s.Quality != online.QLossless || s.URL == "" {
		t.Fatalf("full %+v %v", s, err)
	}
	if f.eapiData["/api/song/enhance/player/url/v1"]["level"] != "lossless" {
		t.Fatalf("level param %v", f.eapiData["/api/song/enhance/player/url/v1"])
	}
	f.set("/api/song/enhance/player/url/v1", "url_trial.json")
	s, _ = p.Resolve(ctx, "186016", online.QHigh)
	if s.Entitlement != online.EntPreview || s.PreviewStartMs != 60000 || s.PreviewEndMs != 90000 {
		t.Fatalf("preview %+v", s)
	}
	f.set("/api/song/enhance/player/url/v1", "url_none.json")
	s, _ = p.Resolve(ctx, "999", online.QHigh)
	if s.Entitlement != online.EntUnavailable || s.URL != "" || !strings.Contains(s.Reason, "付费") {
		t.Fatalf("unavailable %+v", s)
	}
	if _, err := p.Resolve(ctx, "../x", online.QHigh); err == nil {
		t.Fatal("bad id must fail")
	}
}

func TestPlaylistDiscoveryLyrics(t *testing.T) {
	p, f, c := newPlugin(t)
	ctx := context.Background()
	pl, err := p.Playlist(ctx, "7001")
	if err != nil || len(pl.Tracks) != 3 || pl.Owner != "小明" {
		t.Fatalf("playlist %+v %v", pl, err)
	}
	if pl.Tracks[0].Playable != online.EntPreview {
		t.Fatalf("privilege from top-level list %+v", pl.Tracks[0])
	}
	ch, err := p.Charts(ctx)
	if err != nil || len(ch) != 2 || ch[0].Kind != "chart" || ch[0].Subtitle != "每天更新" {
		t.Fatalf("charts %+v %v", ch, err)
	}
	l, err := p.Lyrics(ctx, "186016")
	if err != nil || !l.Synced || !strings.Contains(l.LRC, "故事的小黄花") {
		t.Fatalf("lyrics %+v %v", l, err)
	}
	// anonymous: personalized playlists
	rec, err := p.RecommendPlaylists(ctx)
	if err != nil || rec[0].ID != "netease:8002" {
		t.Fatalf("personalized %+v %v", rec, err)
	}
	c.SetCookie("MUSIC_U", "s")
	rec, _ = p.RecommendPlaylists(ctx)
	if rec[0].ID != "netease:8001" || rec[0].Subtitle != "根据你的口味" {
		t.Fatalf("recommend resource %+v", rec)
	}
	songs, err := p.RecommendTracks(ctx)
	if err != nil || len(songs) != 1 || songs[0].Playable != online.EntFull {
		t.Fatalf("daily %+v %v", songs, err)
	}
	ups, err := p.UserPlaylists(ctx)
	if err != nil || len(ups) != 2 || ups[0].Kind != "liked" || !ups[0].Mine || ups[1].Mine {
		t.Fatalf("user playlists %+v %v", ups, err)
	}
	disc, err := p.DiscoverPlaylists(ctx, "华语", 0, 10)
	if err != nil || len(disc) != 1 {
		t.Fatalf("discover %v", err)
	}
	al, err := p.Album(ctx, "18905")
	if err != nil || len(al.Tracks) != 1 || al.Tracks[0].CoverURL == "" || al.Year != 2003 {
		t.Fatalf("album %+v %v", al, err)
	}
	ar, err := p.Artist(ctx, "6452")
	if err != nil || ar.Name != "周杰伦" || len(ar.TopTracks) != 1 {
		t.Fatalf("artist %+v %v", ar, err)
	}
	f.set("/api/toplist", "risk.json")
	if _, err := p.Charts(ctx); !errors.Is(err, online.ErrRiskControl) {
		t.Fatalf("risk control mapping: %v", err)
	}
	if !bytes.Contains([]byte(strings.Join(f.calls, ",")), []byte("/api/v3/song/detail")) {
		t.Fatal("playlist should fetch missing tracks via song detail")
	}
}

func TestHotSearch(t *testing.T) {
	p, _, _ := newPlugin(t)
	var _ online.HotSearcher = p
	items, err := p.HotSearch(context.Background())
	if err != nil {
		t.Fatalf("hot %v", err)
	}
	if len(items) < 2 || items[0].Keyword != "我不难过" || items[0].Score != 3 {
		t.Fatalf("items %+v", items)
	}
}
