package gateway

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
	"github.com/my-name-is-alan/yimeng/core/internal/online"
	"github.com/my-name-is-alan/yimeng/core/internal/vault"
)

// gwPlugin is an offline platform for gateway tests. Its "CDN" is an
// httptest server; tracks: full, prev (30 s preview), none (unavailable),
// expire (first fetch answers 403 to exercise re-resolve).
type gwPlugin struct {
	env            online.Env
	cdn            string
	expired        atomic.Int32
	userFromCookie bool   // Account() user id = cookie U
	real           []byte // when set, the CDN serves these bytes (a real mp3) for every track
	forbiddenHost  bool   // primary URL answers 200 audio/mpeg with an HTML "Forbidden" page
	noHeader       bool   // when true, Resolve omits Header (App-direct CDN / NetEase-style)
}

func (p *gwPlugin) Manifest() online.Manifest {
	return online.Manifest{ID: "gw", Name: "测试平台", Version: "t",
		Capabilities: []online.Capability{online.CapSearch, online.CapCharts, online.CapRecommend, online.CapUserPlaylists, online.CapStream, online.CapLyrics, online.CapHotSearch},
		LoginMethods: []online.LoginMethod{online.LoginQR, online.LoginCookie},
		QRChannels:   []online.QRChannel{{ID: "app", Label: "App 扫码", App: "测试 App", Hint: "请使用 测试 App 扫码"}},
		Qualities:    []online.Quality{online.QStandard, online.QHigh}}
}

func gwTrack(id, title string, e online.Entitlement) online.Track {
	return online.Track{ID: "gw:" + id, Origin: "gw", Title: title, ArtistName: "歌手", Artists: []online.Ref{{ID: "gw:a1", Name: "歌手"}},
		Album: &online.Ref{ID: "gw:al1", Name: "专辑"}, CoverURL: "https://img.example/c.jpg", DurationMs: 180000, Playable: e}
}

var gwTracks = map[string]online.Track{
	"full": gwTrack("full", "Song Full", online.EntFull), "prev": gwTrack("prev", "Song Preview", online.EntPreview),
	"none": gwTrack("none", "Song Gone", online.EntUnavailable), "expire": gwTrack("expire", "Song Expire", online.EntFull),
	// listed as trial-capable, but the platform serves no URL (paid album)
	"paid": gwTrack("paid", "Song Paid Album", online.EntPreview),
}

func (p *gwPlugin) Search(ctx context.Context, q string, limit int) (online.SearchResult, error) {
	r := online.SearchResult{}
	for _, id := range []string{"full", "prev", "none"} {
		if strings.Contains(strings.ToLower(gwTracks[id].Title), strings.ToLower(q)) {
			r.Tracks = append(r.Tracks, gwTracks[id])
		}
	}
	return r, nil
}
func (p *gwPlugin) Tracks(ctx context.Context, ids []string) ([]online.Track, error) {
	var out []online.Track
	for _, id := range ids {
		if t, ok := gwTracks[id]; ok {
			out = append(out, t)
		}
	}
	return out, nil
}
func (p *gwPlugin) Album(ctx context.Context, id string) (online.Album, error) {
	return online.Album{ID: "gw:" + id, Origin: "gw", Title: "专辑", Tracks: []online.Track{gwTracks["full"]}}, nil
}
func (p *gwPlugin) Artist(ctx context.Context, id string) (online.Artist, error) {
	return online.Artist{ID: "gw:" + id, Origin: "gw", Name: "歌手"}, nil
}
func (p *gwPlugin) Playlist(ctx context.Context, id string) (online.Playlist, error) {
	return online.Playlist{ID: "gw:" + id, Origin: "gw", Name: "歌单 " + id, Kind: "playlist",
		Tracks: []online.Track{gwTracks["full"], gwTracks["prev"], gwTracks["none"]}}, nil
}
func (p *gwPlugin) Lyrics(ctx context.Context, id string) (online.Lyrics, error) {
	return online.Lyrics{Synced: true, LRC: "[00:01.00]第一句\n[00:02.00]第二句\n", Translation: "[00:01.00]line one\n"}, nil
}
func (p *gwPlugin) Charts(ctx context.Context) ([]online.Playlist, error) {
	return []online.Playlist{{ID: "gw:c1", Origin: "gw", Name: "飙升榜", Kind: "chart"}}, nil
}
func (p *gwPlugin) HotSearch(ctx context.Context) ([]online.HotKeyword, error) {
	return []online.HotKeyword{{Keyword: "热搜词", Score: 42, URL: "https://example/hot"}}, nil
}
func (p *gwPlugin) RecommendTracks(ctx context.Context) ([]online.Track, error) {
	if p.env.HTTP.Cookie("S") == "" {
		return nil, online.ErrNeedsLogin
	}
	return []online.Track{gwTracks["full"]}, nil
}
func (p *gwPlugin) RecommendPlaylists(ctx context.Context) ([]online.Playlist, error) {
	return []online.Playlist{{ID: "gw:r1", Origin: "gw", Name: "推荐"}}, nil
}
func (p *gwPlugin) DiscoverPlaylists(ctx context.Context, c string, o, l int) ([]online.Playlist, error) {
	return []online.Playlist{{ID: "gw:d1", Origin: "gw", Name: "发现"}}, nil
}
func (p *gwPlugin) UserPlaylists(ctx context.Context) ([]online.Playlist, error) {
	if p.env.HTTP.Cookie("S") == "" {
		return nil, online.ErrNeedsLogin
	}
	return []online.Playlist{{ID: "gw:liked", Origin: "gw", Name: "我喜欢", Kind: "liked", Mine: true}}, nil
}
func (p *gwPlugin) Resolve(ctx context.Context, id string, q online.Quality) (online.Stream, error) {
	t, ok := gwTracks[id]
	if !ok {
		return online.Stream{}, online.ErrUnavailable
	}
	if id == "paid" {
		return online.Stream{Entitlement: online.EntUnavailable, Reason: "付费专辑歌曲，需要购买"}, nil
	}
	s := online.Stream{Entitlement: t.Playable, Quality: q, Format: "mp3", ExpiresAt: time.Now().Add(time.Hour)}
	if t.Playable != online.EntUnavailable {
		s.URL = p.cdn + "/" + id + ".mp3"
		if p.forbiddenHost {
			s.Alternates = []string{s.URL}
			s.URL = p.cdn + "/forbidden/" + id + ".mp3"
		}
		if !p.noHeader {
			s.Header = map[string]string{"X-Cdn-Auth": "secret-cdn"}
		}
	} else {
		s.Reason = "需要付费购买"
	}
	if t.Playable == online.EntPreview {
		s.PreviewStartMs, s.PreviewEndMs = 30000, 60000
	}
	return s, nil
}
func (p *gwPlugin) StartQR(ctx context.Context) (online.QRChallenge, error) {
	return online.QRChallenge{Key: "k1", Content: "https://example/qr?k=k1", ExpiresAt: time.Now().Add(time.Minute)}, nil
}
func (p *gwPlugin) PollQR(ctx context.Context, key string) (online.QRPoll, error) {
	p.env.HTTP.SetCookie("S", "SECRET-SESSION")
	return online.QRPoll{Status: online.QRConfirmed, Account: &online.Account{UserID: "1", Nickname: "小明"}}, nil
}
func (p *gwPlugin) LoginCookie(ctx context.Context, c string) (online.Account, error) {
	m := online.ParseCookieString(c)
	if m["S"] == "" {
		return online.Account{}, online.ErrNeedsLogin
	}
	p.env.HTTP.ReplaceCookies(m)
	if p.userFromCookie {
		return online.Account{UserID: m["U"], Nickname: "用户" + m["U"]}, nil
	}
	return online.Account{UserID: "1", Nickname: "小明"}, nil
}
func (p *gwPlugin) LoginPassword(context.Context, string, string) (online.Account, error) {
	return online.Account{}, online.ErrNotSupported
}
func (p *gwPlugin) Account(ctx context.Context) (online.Account, error) {
	if p.env.HTTP.Cookie("S") == "" {
		return online.Account{}, online.ErrNeedsLogin
	}
	if p.userFromCookie {
		return online.Account{UserID: p.env.HTTP.Cookie("U"), Nickname: "用户" + p.env.HTTP.Cookie("U")}, nil
	}
	return online.Account{UserID: "1", Nickname: "小明"}, nil
}
func (p *gwPlugin) Refresh(context.Context) error { return nil }

func newOnlineEnv(t *testing.T) (*env, *gwPlugin) { return newOnlineEnvExtra(t) }

// newOnlineEnvExtra also registers extra plugins (built with the gw CDN URL).
func newOnlineEnvExtra(t *testing.T, extra ...func(cdn string) online.Factory) (*env, *gwPlugin) {
	plug := &gwPlugin{}
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !plug.noHeader && r.Header.Get("X-Cdn-Auth") != "secret-cdn" {
			http.Error(w, "no auth", 401)
			return
		}
		if r.URL.Path == "/expire.mp3" && plug.expired.Add(1) == 1 {
			http.Error(w, "expired", 403)
			return
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		if strings.HasPrefix(r.URL.Path, "/forbidden/") {
			io.WriteString(w, "<html>\r\n<head><title>Forbidden</title></head><body>denied</body></html>")
			return
		}
		if plug.real != nil {
			http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(plug.real))
			return
		}
		http.ServeContent(w, r, "", time.Time{}, strings.NewReader("AUDIO-"+strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".mp3")))
	}))
	t.Cleanup(cdn.Close)
	plug.cdn = cdn.URL
	e := newEnvWith(t, func(d *Deps, db *sql.DB, sc *library.Scanner) {
		v, _ := vault.NewSQLStore(db, make([]byte, vault.KeySize))
		reg := online.NewRegistry(db, v, nil, nil)
		reg.Register(func(env online.Env) (online.Plugin, error) { plug.env = env; return plug, nil })
		for _, x := range extra {
			reg.Register(x(plug.cdn))
		}
		if err := reg.Load(context.Background()); err != nil {
			t.Fatal(err)
		}
		svc := online.NewService(reg, sc, online.NewCache(db), nil)
		sc.SetExternalResolver(svc.Materialize)
		d.Online, d.OnlineLib = svc, sc
	})
	return e, plug
}

func TestOnlinePluginsAdmin(t *testing.T) {
	e, _ := newOnlineEnv(t)
	tok := e.token
	res, body := e.do("GET", "/api/v1/plugins", "", tok)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"id":"gw"`) || !strings.Contains(string(body), `"enabled":false`) {
		t.Fatalf("list %d %s", res.StatusCode, body)
	}
	if res, _ := e.do("GET", "/api/plugins", "", ""); res.StatusCode != 401 {
		t.Fatal("auth required")
	}
	if res, _ := e.do("GET", "/api/home", "", tok); res.StatusCode != 200 {
		t.Fatal("home")
	}
	// disabled plugin: stream is refused
	if res, _ := e.do("GET", "/api/stream/gw:full", "", tok); res.StatusCode != 409 {
		t.Fatalf("disabled stream %d", res.StatusCode)
	}
	if res, body := e.do("PUT", "/api/plugins/gw", `{"config":{"quality":"lossless"}}`, tok); res.StatusCode != 400 {
		t.Fatalf("unsupported quality accepted %s", body)
	}
	if res, _ := e.do("PUT", "/api/plugins/nope", `{"enabled":true}`, tok); res.StatusCode != 404 {
		t.Fatal("unknown plugin")
	}
	res, body = e.do("PUT", "/api/plugins/gw", `{"enabled":true,"config":{"quality":"standard","rate_per_min":30}}`, tok)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"enabled":true`) || !strings.Contains(string(body), `"rate_per_min":30`) {
		t.Fatalf("configure %d %s", res.StatusCode, body)
	}
	if res, _ := e.do("POST", "/api/plugins/gw/login", `{"method":"cookie","cookie":"X=1"}`, tok); res.StatusCode != 401 {
		t.Fatal("cookie without session should be rejected")
	}
	res, body = e.do("POST", "/api/plugins/gw/login", `{"method":"cookie","cookie":"S=SECRET-SESSION"}`, tok)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"logged_in":true`) || strings.Contains(string(body), "SECRET") {
		t.Fatalf("cookie login %d %s", res.StatusCode, body)
	}
	if res, _ := e.do("DELETE", "/api/plugins/gw/session", "", tok); res.StatusCode != 204 {
		t.Fatal("logout")
	}
	res, body = e.do("POST", "/api/plugins/gw/login/qr", "", tok)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"image":"data:image/png;base64,`) {
		t.Fatalf("qr %d %.200s", res.StatusCode, body)
	}
	res, body = e.do("GET", "/api/plugins/gw/login/qr/k1", "", tok)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"status":"confirmed"`) {
		t.Fatalf("poll %s", body)
	}
	_, body = e.do("GET", "/api/plugins?check=1", "", tok)
	if !strings.Contains(string(body), `"logged_in":true`) || strings.Contains(string(body), "SECRET") || strings.Contains(string(body), "secret-cdn") {
		t.Fatalf("status leaks or not logged in: %s", body)
	}
	// mode
	_, body = e.do("GET", "/api/settings/mode", "", tok)
	if !strings.Contains(string(body), `"configured":false`) {
		t.Fatalf("mode %s", body)
	}
	if res, _ := e.do("PUT", "/api/settings/mode", `{"mode":"cloud"}`, tok); res.StatusCode != 400 {
		t.Fatal("bad mode")
	}
	_, body = e.do("PUT", "/api/settings/mode", `{"mode":"online"}`, tok)
	if !strings.Contains(string(body), `"mode":"online"`) || !strings.Contains(string(body), `"online_enabled":["gw"]`) {
		t.Fatalf("mode put %s", body)
	}
}

func onlineReady(t *testing.T) *env {
	e, _ := newOnlineEnv(t)
	e.do("PUT", "/api/plugins/gw", `{"enabled":true}`, e.token)
	e.do("POST", "/api/plugins/gw/login", `{"method":"cookie","cookie":"S=SECRET-SESSION"}`, e.token)
	return e
}

func TestOnlineBrowse(t *testing.T) {
	e := onlineReady(t)
	tok := e.token
	_, body := e.do("GET", "/api/search?q=song&online=1", "", tok)
	var sr struct {
		Tracks []library.Track       `json:"tracks"`
		Online []online.OriginResult `json:"online"`
	}
	json.Unmarshal(body, &sr)
	if len(sr.Online) != 1 || sr.Online[0].Result == nil || len(sr.Online[0].Result.Tracks) != 3 {
		t.Fatalf("federated %s", body)
	}
	_, body = e.do("GET", "/api/search?q=song", "", tok)
	if strings.Contains(string(body), `"online"`) {
		t.Fatal("online results without opt-in")
	}
	_, body = e.do("GET", "/api/home", "", tok)
	for _, id := range []string{"gw.daily", "gw.mine", "gw.playlists", "gw.charts"} {
		if !strings.Contains(string(body), `"id":"`+id+`"`) {
			t.Errorf("home missing %s: %s", id, body)
		}
	}
	_, body = e.do("GET", "/api/charts?origin=gw", "", tok)
	if !strings.Contains(string(body), "飙升榜") {
		t.Fatalf("charts %s", body)
	}
	for scope, want := range map[string]string{"mine": "我喜欢", "recommend": "推荐", "discover": "发现"} {
		_, body = e.do("GET", "/api/playlists?scope="+scope, "", tok)
		if !strings.Contains(string(body), want) {
			t.Errorf("scope %s: %s", scope, body)
		}
	}
	if res, _ := e.do("GET", "/api/playlists?scope=bogus", "", tok); res.StatusCode != 400 {
		t.Error("bad scope")
	}
	if res, body := e.do("GET", "/api/playlists", "", tok); res.StatusCode != 200 || !strings.Contains(string(body), `"items"`) {
		t.Error("local playlists unchanged")
	}
	res, body := e.do("GET", "/api/online/gw/playlists/p1", "", tok)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"preview":1`) || !strings.Contains(string(body), `"unavailable":1`) {
		t.Fatalf("playlist detail %s", body)
	}
	for _, k := range []string{"tracks/full", "albums/al1", "artists/a1"} {
		if res, body := e.do("GET", "/api/online/gw/"+k, "", tok); res.StatusCode != 200 {
			t.Errorf("%s: %d %s", k, res.StatusCode, body)
		}
	}
	if res, _ := e.do("GET", "/api/online/gw/widgets/1", "", tok); res.StatusCode != 404 {
		t.Error("bad kind")
	}
	_, body = e.do("GET", "/api/online/favorites", "", tok)
	if !strings.Contains(string(body), `"kind":"liked"`) {
		t.Fatalf("cloud favorites %s", body)
	}
	// lyrics for an online track
	res, body = e.do("GET", "/api/tracks/gw:full/lyrics", "", tok)
	if res.StatusCode != 200 || !strings.Contains(string(body), "第一句") || !strings.Contains(string(body), "line one") {
		t.Fatalf("lyrics %s", body)
	}
}

func TestOnlineCloneAndFavorite(t *testing.T) {
	e := onlineReady(t)
	tok := e.token
	res, body := e.do("POST", "/api/online/gw/playlists/p1/clone", `{"skip_unavailable":true}`, tok)
	if res.StatusCode != 201 || !strings.Contains(string(body), `"skipped":1`) {
		t.Fatalf("clone %d %s", res.StatusCode, body)
	}
	var cr struct {
		Playlist struct{ ID string } `json:"playlist"`
	}
	json.Unmarshal(body, &cr)
	_, body = e.do("GET", "/api/playlists/"+cr.Playlist.ID, "", tok)
	if !strings.Contains(string(body), "gw:full") || !strings.Contains(string(body), "gw:prev") || strings.Contains(string(body), "gw:none") {
		t.Fatalf("cloned items %s", body)
	}
	if res, body := e.do("PUT", "/api/favorites/track/gw:full", "", tok); res.StatusCode != 204 {
		t.Fatalf("favorite online track %d %s", res.StatusCode, body)
	}
	_, body = e.do("GET", "/api/favorites?type=track", "", tok)
	if !strings.Contains(string(body), "gw:full") {
		t.Fatalf("favorites %s", body)
	}
	// materialized tracks stay out of the default (local) library lists
	_, body = e.do("GET", "/api/tracks", "", tok)
	if strings.Contains(string(body), "gw:") {
		t.Fatalf("external tracks leaked into local list %s", body)
	}
	_, body = e.do("GET", "/api/tracks?origin=gw", "", tok)
	if !strings.Contains(string(body), "gw:full") {
		t.Fatalf("origin filter %s", body)
	}
	// metadata editing stays local-only
	if res, _ := e.do("PUT", "/api/overrides/track/gw:full", `{"title":"x"}`, tok); res.StatusCode != 409 {
		t.Fatalf("override on online track: %d", res.StatusCode)
	}
	// cover redirects to the platform CDN
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/albums/gw:al1/cover", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	cl := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	r2, err := cl.Do(req)
	if err != nil || r2.StatusCode != 302 || r2.Header.Get("Location") != "https://img.example/c.jpg" {
		t.Fatalf("cover redirect %v %v", r2.StatusCode, err)
	}
}

func TestOnlineStreamEntitlement(t *testing.T) {
	e := onlineReady(t)
	tok := e.token
	res, body := e.do("GET", "/api/stream/gw:full", "", tok)
	if res.StatusCode != 200 || string(body) != "AUDIO-full" || res.Header.Get("X-Yimeng-Entitlement") != "full" || res.Header.Get("Content-Type") != "audio/mpeg" {
		t.Fatalf("full %d %q %v", res.StatusCode, body, res.Header)
	}
	res, body = e.do("GET", "/api/stream/gw:full", "", tok, "Range", "bytes=6-")
	if res.StatusCode != 206 || string(body) != "full" {
		t.Fatalf("range %d %q", res.StatusCode, body)
	}
	// query token works for <audio>
	if res, _ := e.do("GET", "/api/stream/gw:full?access_token="+tok, "", ""); res.StatusCode != 200 {
		t.Fatal("query token")
	}
	res, body = e.do("GET", "/api/stream/gw:prev", "", tok)
	if res.StatusCode != 200 || string(body) != "AUDIO-prev" || res.Header.Get("X-Yimeng-Entitlement") != "preview" ||
		res.Header.Get("X-Yimeng-Preview-End-Ms") != "60000" {
		t.Fatalf("preview %d %v", res.StatusCode, res.Header)
	}
	res, body = e.do("GET", "/api/stream/gw:none", "", tok)
	if res.StatusCode != 403 || !strings.Contains(string(body), "not_entitled") {
		t.Fatalf("unavailable %d %s", res.StatusCode, body)
	}
	res, body = e.do("GET", "/api/stream/gw:expire", "", tok)
	if res.StatusCode != 200 || string(body) != "AUDIO-expire" {
		t.Fatalf("re-resolve after 403: %d %q", res.StatusCode, body)
	}
	res, body = e.do("GET", "/api/online/resolve/gw:prev", "", tok)
	// headers required → no play_url; CDN host must not appear; stream_url stays Core path
	if res.StatusCode != 200 || !strings.Contains(string(body), `"entitlement":"preview"`) || !strings.Contains(string(body), `"preview_start_ms":30000`) ||
		!strings.Contains(string(body), `"stream_url":"/api/stream/gw:prev"`) || strings.Contains(string(body), "127.0.0.1") ||
		strings.Contains(string(body), `"play_url"`) {
		t.Fatalf("resolve %s", body)
	}
	if res, _ := e.do("GET", "/api/stream/gw:missing", "", tok); res.StatusCode != 404 {
		t.Fatalf("missing %d", res.StatusCode)
	}
}

func TestOnlineStreamTranscode(t *testing.T) {
	e, plug := newOnlineEnv(t)
	if e.tc == nil {
		t.Skip("ffmpeg not available")
	}
	e.do("PUT", "/api/plugins/gw", `{"enabled":true}`, e.token)
	e.do("POST", "/api/plugins/gw/login", `{"method":"cookie","cookie":"S=SECRET-SESSION"}`, e.token)
	plug.real, _ = os.ReadFile("../adapters/local/testdata/notags.mp3")
	// default stays the original passthrough (Range honoured)
	res, body := e.do("GET", "/api/stream/gw:full", "", e.token, "Range", "bytes=0-9")
	if res.StatusCode != 206 || len(body) != 10 || res.Header.Get("X-Stream-Profile") != "original" {
		t.Fatalf("original %d %d %v", res.StatusCode, len(body), res.Header)
	}
	// explicit opus: piped through ffmpeg, never cached, entitlement headers kept
	res, body = e.do("GET", "/api/stream/gw:prev?format=opus&bitrate=96", "", e.token)
	if res.StatusCode != 200 || res.Header.Get("X-Stream-Profile") != "opus-96" || !strings.HasPrefix(string(body), "OggS") ||
		res.Header.Get("X-Yimeng-Entitlement") != "preview" || res.Header.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("opus %d %v %q", res.StatusCode, res.Header, body[:min(8, len(body))])
	}
	// same codec / lossless-from-lossy requests fall back to the original
	for _, q := range []string{"format=mp3&bitrate=128", "format=flac"} {
		res, _ = e.do("GET", "/api/stream/gw:full?"+q, "", e.token)
		if res.Header.Get("X-Stream-Profile") != "original" {
			t.Fatalf("%s: %v", q, res.Header.Get("X-Stream-Profile"))
		}
	}
}

func TestPluginMultiAccount(t *testing.T) {
	e, plug := newOnlineEnv(t)
	tok := e.token
	e.do("PUT", "/api/plugins/gw", `{"enabled":true}`, tok)
	// account A (cookie), then account B: the plugin reports a user id from cookie U
	plug.userFromCookie = true
	e.do("POST", "/api/plugins/gw/login", `{"method":"cookie","cookie":"S=A-SESSION; U=a"}`, tok)
	res, body := e.do("POST", "/api/plugins/gw/login", `{"method":"cookie","cookie":"S=B-SESSION; U=b"}`, tok)
	if res.StatusCode != 200 {
		t.Fatal(string(body))
	}
	var st struct {
		Accounts []struct {
			ID      string
			Default bool
		} `json:"accounts"`
		ActiveAccount string `json:"active_account"`
	}
	_, body = e.do("GET", "/api/plugins/gw", "", tok)
	json.Unmarshal(body, &st)
	if len(st.Accounts) != 2 || st.ActiveAccount != "b" || !st.Accounts[0].Default || strings.Contains(string(body), "SESSION") {
		t.Fatalf("%s", body)
	}
	if plug.env.HTTP.Cookie("S") != "B-SESSION" {
		t.Fatal("B should be active")
	}
	// switch back to A
	if res, body := e.do("POST", "/api/plugins/gw/accounts/a/default", "", tok); res.StatusCode != 200 || !strings.Contains(string(body), `"active_account":"a"`) {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	if plug.env.HTTP.Cookie("S") != "A-SESSION" {
		t.Fatal("A session not loaded:", plug.env.HTTP.Cookie("S"))
	}
	if res, _ := e.do("POST", "/api/plugins/gw/accounts/zzz/default", "", tok); res.StatusCode != 404 {
		t.Fatal("unknown account")
	}
	// remove the active one → falls back to B
	res, body = e.do("DELETE", "/api/plugins/gw/accounts/a", "", tok)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"active_account":"b"`) || plug.env.HTTP.Cookie("S") != "B-SESSION" {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	// logout forgets the last one
	e.do("DELETE", "/api/plugins/gw/session", "", tok)
	_, body = e.do("GET", "/api/plugins/gw", "", tok)
	if strings.Contains(string(body), `"accounts"`) || strings.Contains(string(body), `"logged_in":true`) {
		t.Fatalf("%s", body)
	}
}

func TestOnlineStreamRejectsHTMLAndUsesAlternates(t *testing.T) {
	e, plug := newOnlineEnv(t)
	e.do("PUT", "/api/plugins/gw", `{"enabled":true}`, e.token)
	e.do("POST", "/api/plugins/gw/login", `{"method":"cookie","cookie":"S=SECRET-SESSION"}`, e.token)
	plug.forbiddenHost = true
	res, body := e.do("GET", "/api/stream/gw:full", "", e.token)
	if res.StatusCode != 200 || string(body) != "AUDIO-full" {
		t.Fatalf("alternate not used: %d %q", res.StatusCode, body)
	}
	// every host refuses: a clear JSON error instead of HTML labelled audio
	plug.forbiddenHost = false
	res, body = e.do("GET", "/api/stream/gw:prev?x=1", "", e.token)
	_ = res
	if strings.Contains(string(body), "<html") {
		t.Fatal("html leaked")
	}
}

func TestPluginQRChannel(t *testing.T) {
	e, _ := newOnlineEnv(t)
	e.do("PUT", "/api/plugins/gw", `{"enabled":true}`, e.token)
	res, body := e.do("POST", "/api/plugins/gw/login/qr", `{"channel":"wechat"}`, e.token)
	if res.StatusCode != 400 || !strings.Contains(string(body), "unsupported_channel") {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	res, body = e.do("POST", "/api/plugins/gw/login/qr", `{"channel":"app"}`, e.token)
	if res.StatusCode != 200 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	res, body = e.do("POST", "/api/plugins/gw/login/qr", "", e.token)
	if res.StatusCode != 200 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
}

func TestOnlineStreamDirectRedirect(t *testing.T) {
	e, plug := newOnlineEnv(t)
	e.do("PUT", "/api/plugins/gw", `{"enabled":true}`, e.token)
	e.do("POST", "/api/plugins/gw/login", `{"method":"cookie","cookie":"S=SECRET-SESSION"}`, e.token)
	// NetEase-style: HTTPS CDN URL, no injected headers → 302 (audio bypasses Core).
	plug.noHeader = true
	plug.cdn = "https://m801.music.126.net/test"
	want := plug.cdn + "/full.mp3"

	cl := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/stream/gw:full", nil)
	req.Header.Set("Authorization", "Bearer "+e.token)
	res, err := cl.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 302 || res.Header.Get("Location") != want {
		t.Fatalf("direct 302: %d Location=%q want %q", res.StatusCode, res.Header.Get("Location"), want)
	}
	if res.Header.Get("X-Yimeng-Entitlement") != "full" {
		t.Fatalf("entitlement header %q", res.Header.Get("X-Yimeng-Entitlement"))
	}

	_, body := e.do("GET", "/api/online/resolve/gw:full", "", e.token)
	if !strings.Contains(string(body), `"stream_url":"/api/stream/gw:full"`) ||
		!strings.Contains(string(body), `"play_url":"`+want+`"`) {
		t.Fatalf("resolve play_url %s", body)
	}
	if strings.Contains(string(body), "secret-cdn") || strings.Contains(string(body), `"Header"`) {
		t.Fatalf("cookies/headers leaked: %s", body)
	}
}

func TestOnlineStreamDirectRedirectHTTP(t *testing.T) {
	e, plug := newOnlineEnv(t)
	e.do("PUT", "/api/plugins/gw", `{"enabled":true}`, e.token)
	e.do("POST", "/api/plugins/gw/login", `{"method":"cookie","cookie":"S=SECRET-SESSION"}`, e.token)
	// NetEase often returns http:// CDN hosts; bare GET still works → 302.
	plug.noHeader = true
	plug.cdn = "http://m801.music.126.net/test"
	want := plug.cdn + "/full.mp3"
	cl := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/stream/gw:full", nil)
	req.Header.Set("Authorization", "Bearer "+e.token)
	res, err := cl.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 302 || res.Header.Get("Location") != want {
		t.Fatalf("http direct 302: %d Location=%q want %q", res.StatusCode, res.Header.Get("Location"), want)
	}
	_, body := e.do("GET", "/api/online/resolve/gw:full", "", e.token)
	if !strings.Contains(string(body), `"play_url":"`+want+`"`) {
		t.Fatalf("http play_url %s", body)
	}
}

func TestOnlineHotSearch(t *testing.T) {
	e := onlineReady(t)
	tok := e.token
	res, body := e.do("GET", "/api/v1/online/gw/search/hot", "", tok)
	if res.StatusCode != 200 {
		t.Fatalf("status %d %s", res.StatusCode, body)
	}
	var out struct {
		Origin string              `json:"origin"`
		Items  []online.HotKeyword `json:"items"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.Origin != "gw" || len(out.Items) != 1 || out.Items[0].Keyword != "热搜词" {
		t.Fatalf("body %s", body)
	}
	if res, _ := e.do("GET", "/api/online/unknown/search/hot", "", tok); res.StatusCode != 404 {
		t.Fatalf("unknown origin %d", res.StatusCode)
	}
	// disabled plugin
	e.do("PUT", "/api/plugins/gw", `{"enabled":false}`, tok)
	if res, body := e.do("GET", "/api/online/gw/search/hot", "", tok); res.StatusCode != 409 || !strings.Contains(string(body), "plugin_disabled") {
		t.Fatalf("disabled %d %s", res.StatusCode, body)
	}
}
