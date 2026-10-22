package kugou

import (
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
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/online"
)

func TestSignVectors(t *testing.T) {
	if got := signAndroid(map[string]string{"b": "2", "a": "1", "clienttime": "100"}, `{"x":1}`); got != "312b317120f750c18ec00f6e1ab9bcdb" {
		t.Fatalf("android sig %s", got)
	}
	if got := signWeb(map[string]string{"b": "2", "a": "1"}, ""); got != "70ccbef64fdcc9271fe883d1d7f07395" {
		t.Fatalf("web sig %s", got)
	}
	if got := signKey("abc", "123", ""); got != "89993ea605d5d20555daa94274169f28" {
		t.Fatalf("key %s", got)
	}
	if got := signParamsKey("1700000000"); got != "a1f65b6a8fe7e191521406ce8661ae02" {
		t.Fatalf("params key %s", got)
	}
	if got := calcMid("550e8400-e29b-41d4-a716-446655440000"); got != "308741372901437977228425563242293240765" {
		t.Fatalf("mid %s", got)
	}
}

func TestCryptoRoundTrip(t *testing.T) {
	ct, key := devAES([]byte(`{"a":1}`))
	if len(key) != 6 {
		t.Fatal(key)
	}
	raw, _ := b64(ct)
	if pt, err := devAESDecrypt(raw, key); err != nil || string(pt) != `{"a":1}` {
		t.Fatalf("dev aes %q %v", pt, err)
	}
	h, temp := aesTemp([]byte("tok"))
	if pt, err := aesTempDecrypt(h, temp); err != nil || string(pt) != "tok" {
		t.Fatalf("temp aes %q %v", pt, err)
	}
	if len(rsaRaw([]byte("x"))) != 256 || len(rsaPKCS1([]byte("x"))) != 256 {
		t.Fatal("rsa length")
	}
}

// fake routes by path; every Kugou host is rewritten to it.
type fake struct {
	mu     sync.Mutex
	routes map[string]string // path → testdata file, or "json:<literal>"
	hits   []*http.Request
}

func (f *fake) set(path, v string) { f.mu.Lock(); f.routes[path] = v; f.mu.Unlock() }

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.hits = append(f.hits, r)
	v, ok := f.routes[r.URL.Path]
	f.mu.Unlock()
	if r.URL.Query().Get("signature") == "" && r.URL.Path != "/risk/v2/r_register_dev" {
		http.Error(w, "unsigned", 400)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	if lit, ok := strings.CutPrefix(v, "json:"); ok {
		w.Write([]byte(lit))
		return
	}
	b, err := os.ReadFile("testdata/" + v + ".json")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Write(b)
}

func (f *fake) last(path string) *http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.hits) - 1; i >= 0; i-- {
		if f.hits[i].URL.Path == path {
			return f.hits[i]
		}
	}
	return nil
}

func newPlugin(t *testing.T) (*Plugin, *fake, *online.Client) {
	f := &fake{routes: map[string]string{
		"/v2/search/song": "search_song", "/v1/search/album": "searchalbum", "/v1/search/author": "searchauthor",
		"/v1/search/special": "searchspecial", "/ocean/v6/rank/list": "rank_list", "/openapi/kmr/v2/rank/audio": "rank_audio",
		"/v2/special_recommend": "special_recommend", "/everyday_song_recommend": "everyday",
		"/pubsongs/v2/get_other_list_file_nofilt": "pl_tracks", "/v3/get_list_info": "pl_detail",
		"/v2/get_res_privilege/lite": "privilege", "/v1/search": "lyric_search", "/download": "lyric",
		"/kmr/v2/albums": "album", "/v1/album_audio/lite": "album_songs", "/kmr/v3/author": "artist",
		"/kmr/v1/audio_group/author": "artist_audios", "/v2/qrcode": "qrkey", "/v5/url": "url_preview",
	}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c := online.NewClient("kugou", 6000, srv.Client())
	c.SetCookie(kDfid, "testdfid") // skip device registration
	c.SetCookie(kDfidAt, "9999999999")
	pl, err := New(Options{Rewrite: func(string) string { return srv.URL }})(online.Env{HTTP: c})
	if err != nil {
		t.Fatal(err)
	}
	return pl.(*Plugin), f, c
}

func TestManifestAndInterfaces(t *testing.T) {
	p, _, _ := newPlugin(t)
	m := p.Manifest()
	if m.Skeleton || !m.Has(online.CapStream) || !m.Has(online.CapUserPlaylists) {
		t.Fatalf("manifest %+v", m)
	}
	var _ online.Authenticator = p
	var _ online.SMSAuthenticator = p
	var _ online.Catalog = p
	var _ online.Discovery = p
	var _ online.UserLibrary = p
	var _ online.Streamer = p
}

func TestDeviceIdentity(t *testing.T) {
	p, f, c := newPlugin(t)
	if _, err := p.Search(context.Background(), "晴天", 5); err != nil {
		t.Fatal(err)
	}
	g, mid := c.Cookie(kGUID), c.Cookie(kMid)
	if g == "" || mid != calcMid(g) {
		t.Fatalf("device ids guid=%q mid=%q", g, mid)
	}
	r := f.last("/v2/search/song")
	if r.URL.Query().Get("mid") != mid || r.Header.Get("dfid") != "testdfid" || r.Header.Get("x-router") != "complexsearch.kugou.com" {
		t.Fatalf("request ids %v %v", r.URL.Query(), r.Header)
	}
	for k := range c.Cookies() {
		if !strings.HasPrefix(k, online.DevicePrefix) {
			t.Fatalf("anonymous jar has session key %q", k)
		}
	}
}

func TestRegisterDevice(t *testing.T) {
	p, f, c := newPlugin(t)
	c.ReplaceCookies(nil)
	f.mu.Lock()
	f.routes["/risk/v2/r_register_dev"] = "dyn"
	f.mu.Unlock()
	// The fake can't decrypt (key is RSA-wrapped), so registration fails
	// gracefully and the plugin keeps working with dfid "-".
	if _, err := p.Search(context.Background(), "x", 5); err != nil {
		t.Fatal(err)
	}
	if got := f.last("/v2/search/song").URL.Query().Get("dfid"); got != "-" {
		t.Fatalf("dfid %q", got)
	}
}

func TestSearchMapping(t *testing.T) {
	p, _, _ := newPlugin(t)
	res, err := p.Search(context.Background(), "周杰伦 晴天", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tracks) == 0 || len(res.Albums) != 1 || len(res.Artists) != 1 || len(res.Playlists) != 1 {
		t.Fatalf("counts %d %d %d %d", len(res.Tracks), len(res.Albums), len(res.Artists), len(res.Playlists))
	}
	tr := res.Tracks[0]
	if tr.ID != "kugou:b3a52a7a958bf0aed0ebfba2e9a818b7-966846-32100650" || tr.Title != "晴天" || tr.ArtistName != "周杰伦" ||
		tr.Album == nil || tr.Album.ID != "kugou:album-966846" || tr.DurationMs != 269000 || !tr.VIP || tr.Playable != online.EntPreview ||
		tr.MaxQuality != online.QHiRes || !strings.HasPrefix(tr.CoverURL, "https://") || strings.Contains(tr.CoverURL, "{size}") {
		t.Fatalf("track %+v album %+v", tr, tr.Album)
	}
	if res.Artists[0].ID != "kugou:artist-3520" || res.Albums[0].Title == "" || !strings.HasPrefix(res.Playlists[0].ID, "kugou:pl-collection_") {
		t.Fatalf("others %+v %+v %+v", res.Artists[0], res.Albums[0], res.Playlists[0])
	}
}

func TestResolveEntitlement(t *testing.T) {
	p, f, _ := newPlugin(t)
	id := "b3a52a7a958bf0aed0ebfba2e9a818b7-966846-32100650"
	ctx := context.Background()

	// VIP song, anonymous: full request is denied, preview clip is returned.
	var calls []string
	f.set("/v5/url", "url_denied")
	srvHook := func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, h := range f.hits {
			if h.URL.Path == "/v5/url" {
				calls = append(calls, h.URL.Query().Get("IsFreePart")+"/"+h.URL.Query().Get("quality"))
			}
		}
	}
	// denied for full, preview for IsFreePart=1: emulate by switching per call
	st, err := p.resolveWith(ctx, id, online.QLossless, map[string]string{"0": "url_denied", "1": "url_preview"}, f)
	if err != nil {
		t.Fatal(err)
	}
	srvHook()
	if st.Entitlement != online.EntPreview || st.PreviewStartMs != 0 || st.PreviewEndMs != 60000 || st.URL == "" || st.DurationMs != 269000 {
		t.Fatalf("preview stream %+v", st)
	}
	if strings.Join(calls, ",") != "0/flac,0/128,1/128" {
		t.Fatalf("call order %v", calls)
	}
	if k := f.last("/v5/url").URL.Query().Get("key"); k != signKey("b3a52a7a958bf0aed0ebfba2e9a818b7", p.c.Cookie(kMid), "") {
		t.Fatalf("song key %s", k)
	}

	// Entitled: full at the requested quality.
	st, err = p.resolveWith(ctx, id, online.QLossless, map[string]string{"0": "url_full", "1": "url_preview"}, f)
	if err != nil || st.Entitlement != online.EntFull || st.Format != "flac" || st.Quality != online.QLossless || st.PreviewEndMs != 0 {
		t.Fatalf("full stream %+v %v", st, err)
	}

	// A clip URL is never reported as full.
	st, _ = p.resolveWith(ctx, id, online.QStandard, map[string]string{"0": "url_preview", "1": "url_preview"}, f)
	if st.Entitlement != online.EntPreview {
		t.Fatalf("clip as full: %+v", st)
	}

	// Nothing offered: unavailable.
	st, err = p.resolveWith(ctx, id, online.QStandard, map[string]string{"0": "url_denied", "1": "url_denied"}, f)
	if err != nil || st.Entitlement != online.EntUnavailable || st.URL != "" {
		t.Fatalf("unavailable %+v %v", st, err)
	}
}

// resolveWith swaps the /v5/url fixture by IsFreePart via a wrapping server.
func (p *Plugin) resolveWith(ctx context.Context, id string, q online.Quality, byFree map[string]string, f *fake) (online.Stream, error) {
	f.mu.Lock()
	f.hits = nil
	f.mu.Unlock()
	old := p.opts.Rewrite
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v5/url" {
			f.set("/v5/url", byFree[r.URL.Query().Get("IsFreePart")])
		}
		f.ServeHTTP(w, r)
	}))
	defer srv.Close()
	p.opts.Rewrite = func(string) string { return srv.URL }
	defer func() { p.opts.Rewrite = old }()
	return p.Resolve(ctx, id, q)
}

func TestTracksAlbumArtistPlaylist(t *testing.T) {
	p, _, _ := newPlugin(t)
	ctx := context.Background()
	ts, err := p.Tracks(ctx, []string{"b3a52a7a958bf0aed0ebfba2e9a818b7-966846-32100650"})
	if err != nil || len(ts) != 1 || ts[0].Title != "晴天" || ts[0].ArtistName != "周杰伦" || ts[0].DurationMs != 269000 || ts[0].MaxQuality != online.QHiRes {
		t.Fatalf("tracks %+v %v", ts, err)
	}
	if _, err := p.Tracks(ctx, []string{"nothex"}); !errors.Is(err, online.ErrBadInput) {
		t.Fatalf("bad id %v", err)
	}
	al, err := p.Album(ctx, "album-966846")
	if err != nil || al.Title != "叶惠美" || len(al.Tracks) != 2 || al.Tracks[0].TrackNo != 1 || al.Year != 2003 {
		t.Fatalf("album %+v %v", al, err)
	}
	ar, err := p.Artist(ctx, "artist-3520")
	if err != nil || ar.Name != "周杰伦" || len(ar.TopTracks) != 2 || ar.TopTracks[0].Title != "晴天" {
		t.Fatalf("artist %+v %v", ar, err)
	}
	pl, err := p.Playlist(ctx, "pl-collection_3_427832380_22_0")
	if err != nil || pl.Name == "" || len(pl.Tracks) != 2 || pl.Tracks[0].Title == "" || pl.Tracks[0].ArtistName == "" || pl.Tracks[0].Playable != online.EntFull {
		t.Fatalf("playlist %+v %v", pl, err)
	}
	ch, err := p.Playlist(ctx, "rank-8888")
	if err != nil || ch.Name != "TOP500" || ch.Kind != "chart" || len(ch.Tracks) != 2 || ch.Tracks[0].DurationMs == 0 {
		t.Fatalf("chart %+v %v", ch, err)
	}
	if _, err := p.Playlist(ctx, "weird"); !errors.Is(err, online.ErrBadInput) {
		t.Fatal(err)
	}
}

func TestDiscoveryAndLyrics(t *testing.T) {
	p, _, _ := newPlugin(t)
	ctx := context.Background()
	cs, err := p.Charts(ctx)
	if err != nil || len(cs) != 3 || cs[0].ID != "kugou:rank-8888" || cs[0].CoverURL == "" {
		t.Fatalf("charts %+v %v", cs, err)
	}
	rt, err := p.RecommendTracks(ctx)
	if err != nil || len(rt) != 2 || rt[0].Title != "院子里的花" {
		t.Fatalf("recommend %+v %v", rt, err)
	}
	rp, err := p.RecommendPlaylists(ctx)
	if err != nil || len(rp) != 2 || rp[0].PlayCount == 0 {
		t.Fatalf("playlists %+v %v", rp, err)
	}
	if _, err := p.DiscoverPlaylists(ctx, "0", 30, 30); err != nil {
		t.Fatal(err)
	}
	ly, err := p.Lyrics(ctx, "b3a52a7a958bf0aed0ebfba2e9a818b7-966846-32100650")
	if err != nil || !ly.Synced || !strings.Contains(ly.LRC, "晴天") || strings.Contains(ly.LRC, "\r") {
		t.Fatalf("lyrics %+v %v", ly, err)
	}
	if _, err := p.UserPlaylists(ctx); !errors.Is(err, online.ErrNeedsLogin) {
		t.Fatalf("user playlists anon: %v", err)
	}
}

func TestQRAndCookieLogin(t *testing.T) {
	p, f, c := newPlugin(t)
	ctx := context.Background()
	ch, err := p.StartQR(ctx)
	if err != nil || ch.Key != "testqrkey1001" || ch.Content != qrPage+"?qrcode=testqrkey1001" || ch.ExpiresAt.Before(time.Now()) {
		t.Fatalf("qr %+v %v", ch, err)
	}
	for status, want := range map[string]online.QRStatus{"1": online.QRWaiting, "2": online.QRScanned, "0": online.QRExpired} {
		f.set("/v2/get_userinfo_qrcode", `json:{"status":1,"error_code":0,"data":{"status":`+status+`,"nickname":"N"}}`)
		if r, err := p.PollQR(ctx, ch.Key); err != nil || r.Status != want {
			t.Fatalf("poll %s: %+v %v", status, r, err)
		}
	}
	f.set("/v2/get_userinfo_qrcode", `json:{"status":1,"error_code":0,"data":{"status":4,"nickname":"Alan","token":"tok123","userid":987}}`)
	f.set("/v3/get_my_info", `json:{"status":1,"error_code":0,"data":{"nickname":"Alan","pic":"http://x/{size}/a.jpg","userid":987,"vip_type":0}}`)
	f.set("/v1/get_union_vip", `json:{"status":1,"error_code":0,"data":{"is_vip":1}}`)
	r, err := p.PollQR(ctx, ch.Key)
	if err != nil || r.Status != online.QRConfirmed || r.Account == nil || r.Account.Nickname != "Alan" || !r.Account.VIP {
		t.Fatalf("confirm %+v %v", r, err)
	}
	if c.Cookie(kToken) != "tok123" || c.Cookie(kUserID) != "987" || c.Cookie(kGUID) == "" {
		t.Fatalf("session/device after login %v", c.Cookies())
	}
	if q := f.last("/v3/get_my_info").URL.Query(); q.Get("token") != "tok123" || q.Get("userid") != "987" {
		t.Fatalf("logged-in default params %v", q)
	}
	// VIP accounts see VIP songs as fully playable.
	res, _ := p.Search(ctx, "x", 5)
	if res.Tracks[0].Playable != online.EntFull {
		t.Fatalf("vip playable %+v", res.Tracks[0])
	}
	f.set("/v7/get_all_list", `json:{"status":1,"error_code":0,"data":{"info":[{"name":"我喜欢","count":3,"global_collection_id":"collection_3_987_2_0","list_create_userid":987,"pic":""},{"name":"Road","count":1,"global_collection_id":"collection_3_1_9_0","list_create_userid":1}]}}`)
	ups, err := p.UserPlaylists(ctx)
	if err != nil || len(ups) != 2 || ups[0].Kind != "liked" || !ups[0].Mine || ups[1].Mine {
		t.Fatalf("user playlists %+v %v", ups, err)
	}

	// cookie login: bad input keeps the old session
	if _, err := p.LoginCookie(ctx, "foo=bar"); !errors.Is(err, online.ErrBadInput) {
		t.Fatal(err)
	}
	f.set("/v3/get_my_info", `json:{"status":0,"error_code":20018,"data":null}`)
	if _, err := p.LoginCookie(ctx, "KuGoo=KugooID=55&t=abc&a_id=1"); !errors.Is(err, online.ErrNeedsLogin) {
		t.Fatalf("expired cookie %v", err)
	}
	if c.Cookie(kToken) != "tok123" {
		t.Fatal("failed cookie login clobbered session")
	}
	if _, err := p.LoginPassword(ctx, "a", "b"); !errors.Is(err, online.ErrNotSupported) {
		t.Fatal(err)
	}
}

func TestSMSLoginAndRefresh(t *testing.T) {
	p, f, c := newPlugin(t)
	ctx := context.Background()
	f.set("/v7/send_mobile_code", `json:{"status":1,"error_code":0,"data":{}}`)
	if err := p.SendSMS(ctx, "13800000000"); err != nil {
		t.Fatal(err)
	}
	var sent map[string]any
	json.NewDecoder(f.last("/v7/send_mobile_code").Body).Decode(&sent)
	f.set("/v7/login_by_verifycode", `json:{"status":1,"error_code":0,"data":{"token":"smstok","userid":42,"vip_type":0}}`)
	f.set("/v3/get_my_info", `json:{"status":1,"error_code":0,"data":{"nickname":"P","userid":42}}`)
	f.set("/v1/get_union_vip", `json:{"status":1,"error_code":0,"data":{"is_vip":0}}`)
	acc, err := p.LoginSMS(ctx, "13800000000", "1234")
	if err != nil || acc.Nickname != "P" || acc.VIP || c.Cookie(kToken) != "smstok" {
		t.Fatalf("sms login %+v %v", acc, err)
	}
	if q := f.last("/v7/login_by_verifycode"); q.URL.Query().Get("signature") == "" {
		t.Fatal("unsigned login")
	}
	f.set("/v5/login_by_token", `json:{"status":1,"error_code":0,"data":{"token":"newtok","userid":42}}`)
	if err := p.Refresh(ctx); err != nil || c.Cookie(kToken) != "newtok" {
		t.Fatalf("refresh %v %v", err, c.Cookies())
	}
	// captcha challenge → risk control
	f.set("/v7/send_mobile_code", `json:{"status":0,"error_code":30791,"data":{}}`)
	if err := p.SendSMS(ctx, "13800000000"); !errors.Is(err, online.ErrRiskControl) {
		t.Fatalf("captcha %v", err)
	}
}

func b64(s string) ([]byte, error) { return base64.StdEncoding.DecodeString(s) }
