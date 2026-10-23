package qq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/my-name-is-alan/yimeng/core/internal/online"
)

func TestHashes(t *testing.T) {
	// ptqrtoken / g_tk reference values (same algorithm as ptlogin's JS).
	if hash33("abc") != 108966 || gtk("abc") != 193485963 {
		t.Fatalf("hash33=%d gtk=%d", hash33("abc"), gtk("abc"))
	}
}

type fake struct {
	mu      sync.Mutex
	subs    map[string]string // "module.method" (+":type" for search) → fixture or "json:" literal
	vkey    func(files []string) map[string]string
	bodies  []map[string]json.RawMessage
	cookies []string
}

func (f *fake) set(k, v string) { f.mu.Lock(); f.subs[k] = v; f.mu.Unlock() }

func (f *fake) sub(key string) json.RawMessage {
	f.mu.Lock()
	v, ok := f.subs[key]
	f.mu.Unlock()
	if !ok {
		return json.RawMessage(`{"code":500001,"data":{}}`)
	}
	if lit, ok := strings.CutPrefix(v, "json:"); ok {
		return json.RawMessage(lit)
	}
	b, err := os.ReadFile("testdata/" + v + ".json")
	if err != nil {
		panic(err)
	}
	return b
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/cgi-bin/musicu.fcg":
		var body map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.bodies = append(f.bodies, body)
		f.cookies = append(f.cookies, r.Header.Get("Cookie"))
		f.mu.Unlock()
		out := map[string]json.RawMessage{"code": json.RawMessage("0")}
		for k, raw := range body {
			if k == "comm" {
				continue
			}
			var m mreq
			json.Unmarshal(raw, &m)
			key := m.Module + "." + m.Method
			switch {
			case m.Method == "DoSearchForQQMusicDesktop":
				key = fmt.Sprintf("search_%v", m.Param["search_type"])
			case key == "vkey.GetVkeyServer.CgiGetVkey" && f.vkey != nil:
				var files []string
				for _, x := range m.Param["filename"].([]any) {
					files = append(files, x.(string))
				}
				purls := f.vkey(files)
				var infos []map[string]any
				for _, fn := range files {
					infos = append(infos, map[string]any{"filename": fn, "purl": purls[fn]})
				}
				b, _ := json.Marshal(map[string]any{"code": 0, "data": map[string]any{"sip": []string{"http://ws.stream.test/", "http://cdn.stream.test/"}, "midurlinfo": infos}})
				out[k] = b
				continue
			}
			out[k] = f.sub(key)
		}
		json.NewEncoder(w).Encode(out)
	case "/cgi-bin/xlogin":
		http.SetCookie(w, &http.Cookie{Name: "pt_login_sig", Value: "LSIG", Path: "/"})
	case "/ssl/ptqrshow":
		http.SetCookie(w, &http.Cookie{Name: "qrsig", Value: "QRSIG", Path: "/"})
		w.Write([]byte("\x89PNG fake"))
	case "/ssl/ptqrlogin":
		if c, _ := r.Cookie("qrsig"); c == nil || r.URL.Query().Get("ptqrtoken") != fmt.Sprint(hash33("QRSIG")) {
			http.Error(w, "bad token", 400)
			return
		}
		f.mu.Lock()
		state := f.subs["ptqr"]
		f.mu.Unlock()
		if state == "0" {
			fmt.Fprintf(w, "ptuiCB('0','0','https://ssl.ptlogin2.qq.com/check_sig?x=1','0','登录成功！', 'Alan')")
			return
		}
		fmt.Fprintf(w, "ptuiCB('%s','0','','0','msg', '')", state)
	case "/check_sig":
		http.SetCookie(w, &http.Cookie{Name: "p_skey", Value: "PSKEY", Path: "/"})
		w.Header().Set("Location", "https://graph.qq.com/oauth2.0/login_jump")
		w.WriteHeader(302)
	case "/oauth2.0/login_jump":
		w.Write([]byte("ok"))
	case "/oauth2.0/authorize":
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), fmt.Sprintf("g_tk=%d", gtk("PSKEY"))) {
			http.Error(w, "bad g_tk", 400)
			return
		}
		w.Header().Set("Location", "https://y.qq.com/portal/wx_redirect.html?login_type=1&code=OAUTHCODE&state=state")
		w.WriteHeader(302)
	default:
		http.NotFound(w, r)
	}
}

func newPlugin(t *testing.T) (*Plugin, *fake, *online.Client) {
	f := &fake{subs: map[string]string{
		"search_0": "search_0", "search_1": "search_1", "search_2": "search_2", "search_3": "search_3",
		"music.trackInfo.UniformRuleCtrl.CgiGetTrackInfo":     "trackinfo",
		"music.musichallSong.PlayLyricInfo.GetPlayLyricInfo":  "lyric",
		"musicToplist.ToplistInfoServer.GetAll":               "toplists",
		"musicToplist.ToplistInfoServer.GetDetail":            "top",
		"playlist.HotRecommendServer.get_hot_recommend":       "hotpl",
		"playlist.PlayListPlazaServer.get_playlist_by_tag":    "plaza",
		"srf_diss_info.DissInfoServer.CgiGetDiss":             "diss",
		"music.musichallAlbum.AlbumInfoServer.GetAlbumDetail": "album",
		"music.musichallAlbum.AlbumSongList.GetAlbumSongList": "albumsongs",
		"musichall.song_list_server.GetSingerSongList":        "singer",
		"newsong.NewSongServer.get_new_song_info":             "newsongs",
		"music.musicsearch.HotkeyService.GetHotkeyForQQMusicMobile": "hotkey",
		"music.musichallAlbum.AlbumListServer.GetAlbumList":         "singer_albums",
	}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c := online.NewClient("qq", 6000, srv.Client())
	pl, err := New(Options{Rewrite: func(string) string { return srv.URL }})(online.Env{HTTP: c})
	if err != nil {
		t.Fatal(err)
	}
	return pl.(*Plugin), f, c
}

func TestManifestAndInterfaces(t *testing.T) {
	p, _, _ := newPlugin(t)
	if m := p.Manifest(); m.Skeleton || !m.Has(online.CapStream) || !m.Has(online.CapHotSearch) || !m.Has(online.CapArtistAlbums) {
		t.Fatal(m)
	}
	var _ online.Authenticator = p
	var _ online.Catalog = p
	var _ online.Discovery = p
	var _ online.UserLibrary = p
	var _ online.HotSearcher = p
	var _ online.ArtistAlbumer = p
	var _ online.Streamer = p
}

func TestSearch(t *testing.T) {
	p, f, _ := newPlugin(t)
	res, err := p.Search(context.Background(), "晴天", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tracks) != 2 || len(res.Albums) != 1 || len(res.Artists) != 1 || len(res.Playlists) != 1 {
		t.Fatalf("counts %d %d %d %d", len(res.Tracks), len(res.Albums), len(res.Artists), len(res.Playlists))
	}
	tr := res.Tracks[0]
	if tr.ID != "qq:0039MnYb0qxYhV" || tr.Title != "晴天" || tr.ArtistName != "周杰伦" || tr.DurationMs != 269000 || !tr.VIP ||
		tr.Playable != online.EntPreview || tr.Album == nil || tr.Album.ID != "qq:album-000MkMni19ClKG" || !strings.HasPrefix(tr.CoverURL, "https://y.gtimg.cn/") {
		t.Fatalf("track %+v", tr)
	}
	var comm map[string]any
	json.Unmarshal(f.bodies[0]["comm"], &comm)
	if comm["ct"] != "19" {
		t.Fatalf("search must use desktop comm: %v", comm)
	}
}

func TestResolveEntitlement(t *testing.T) {
	p, f, _ := newPlugin(t)
	ctx := context.Background()
	// Paid song, non-VIP: every full file denied → official RS02 trial.
	var asked [][]string
	f.vkey = func(files []string) map[string]string {
		asked = append(asked, files)
		out := map[string]string{}
		for _, fn := range files {
			if strings.HasPrefix(fn, "RS02") {
				out[fn] = fn + "?vkey=T"
			}
		}
		return out
	}
	st, err := p.Resolve(ctx, "0039MnYb0qxYhV", online.QLossless)
	if err != nil {
		t.Fatal(err)
	}
	if st.Entitlement != online.EntPreview || st.PreviewStartMs != 84346 || st.PreviewEndMs != 142843 ||
		st.URL != "http://cdn.stream.test/RS02064YyRNb0gDV4P.mp3?vkey=T" || st.DurationMs != 269000 {
		t.Fatalf("preview %+v", st)
	}
	if len(asked) != 2 || strings.Join(asked[0], ",") != "F0000039MnYb0qxYhV003Qui1q2u1Zho.flac,M8000039MnYb0qxYhV003Qui1q2u1Zho.mp3,M5000039MnYb0qxYhV003Qui1q2u1Zho.mp3,C4000039MnYb0qxYhV003Qui1q2u1Zho.m4a" {
		t.Fatalf("vkey asks %v", asked)
	}
	// Requested quality caps the ladder; granted file → full.
	asked = nil
	f.vkey = func(files []string) map[string]string {
		asked = append(asked, files)
		return map[string]string{"M5000039MnYb0qxYhV003Qui1q2u1Zho.mp3": "M500x.mp3?vkey=F", "C4000039MnYb0qxYhV003Qui1q2u1Zho.m4a": "C400x.m4a?vkey=F"}
	}
	st, err = p.Resolve(ctx, "0039MnYb0qxYhV", online.QHigh)
	if err != nil || st.Entitlement != online.EntFull || st.Format != "mp3" || st.BitrateKbps != 128 || st.PreviewEndMs != 0 || len(asked[0]) != 3 {
		t.Fatalf("full %+v %v %v", st, err, asked)
	}
	// Nothing granted, trial denied too → unavailable.
	f.vkey = func([]string) map[string]string { return nil }
	st, err = p.Resolve(ctx, "0039MnYb0qxYhV", online.QStandard)
	if err != nil || st.Entitlement != online.EntUnavailable || st.URL != "" {
		t.Fatalf("unavailable %+v %v", st, err)
	}
	if _, err := p.Resolve(ctx, "bad:id", online.QStandard); !errors.Is(err, online.ErrBadInput) {
		t.Fatal(err)
	}
}

func TestCatalogDiscovery(t *testing.T) {
	p, _, _ := newPlugin(t)
	ctx := context.Background()
	al, err := p.Album(ctx, "album-000MkMni19ClKG")
	if err != nil || al.Title != "叶惠美" || len(al.Tracks) != 2 || al.Year != 2003 || al.ArtistName != "周杰伦" {
		t.Fatalf("album %+v %v", al, err)
	}
	ar, err := p.Artist(ctx, "artist-0025NhlN2yWrP4")
	if err != nil || ar.Name != "周杰伦" || len(ar.TopTracks) != 2 {
		t.Fatalf("artist %+v %v", ar, err)
	}
	pl, err := p.Playlist(ctx, "pl-7256912512")
	if err != nil || pl.Name == "" || len(pl.Tracks) != 2 || pl.Mine {
		t.Fatalf("playlist %+v %v", pl, err)
	}
	ch, err := p.Playlist(ctx, "top-26")
	if err != nil || ch.Kind != "chart" || ch.Name != "热歌榜" || len(ch.Tracks) != 2 {
		t.Fatalf("chart %+v %v", ch, err)
	}
	cs, err := p.Charts(ctx)
	if err != nil || len(cs) != 2 || !strings.HasPrefix(cs[0].ID, "qq:top-") {
		t.Fatalf("charts %+v %v", cs, err)
	}
	rt, err := p.RecommendTracks(ctx)
	if err != nil || len(rt) != 2 {
		t.Fatalf("recommend %v %v", rt, err)
	}
	rp, err := p.RecommendPlaylists(ctx)
	if err != nil || len(rp) == 0 || !strings.HasPrefix(rp[0].ID, "qq:pl-") {
		t.Fatalf("hot %+v %v", rp, err)
	}
	dp, err := p.DiscoverPlaylists(ctx, "", 0, 2)
	if err != nil || len(dp) != 2 {
		t.Fatalf("plaza %+v %v", dp, err)
	}
	ly, err := p.Lyrics(ctx, "0039MnYb0qxYhV")
	if err != nil || !ly.Synced || !strings.Contains(ly.LRC, "晴天") {
		t.Fatalf("lyrics %+v %v", ly, err)
	}
	ts, err := p.Tracks(ctx, []string{"0039MnYb0qxYhV"})
	if err != nil || len(ts) != 1 || ts[0].MaxQuality != online.QLossless {
		t.Fatalf("tracks %+v %v", ts, err)
	}
	if _, err := p.UserPlaylists(ctx); !errors.Is(err, online.ErrNeedsLogin) {
		t.Fatal(err)
	}
	if _, err := p.Playlist(ctx, "x"); !errors.Is(err, online.ErrBadInput) {
		t.Fatal(err)
	}
}

func TestQRLogin(t *testing.T) {
	p, f, c := newPlugin(t)
	ctx := context.Background()
	ch, err := p.StartQR(ctx)
	if err != nil || ch.Key != "QRSIG|LSIG" || !strings.HasPrefix(ch.ImageURL, "data:image/png;base64,") {
		t.Fatalf("qr %+v %v", ch, err)
	}
	for code, want := range map[string]online.QRStatus{"66": online.QRWaiting, "67": online.QRScanned, "65": online.QRExpired} {
		f.set("ptqr", code)
		if r, err := p.PollQR(ctx, ch.Key); err != nil || r.Status != want {
			t.Fatalf("poll %s %+v %v", code, r, err)
		}
	}
	f.set("ptqr", "0")
	f.set("QQConnectLogin.LoginServer.QQLogin", `json:{"code":0,"data":{"musicid":12345,"musickey":"Q_H_L_KEY","refresh_key":"RK","refresh_token":"RT"}}`)
	f.set("music.UserInfo.userInfoServer.GetLoginUserInfo", `json:{"code":0,"data":{"info":{"nick":"阿伦","headpic":"http://x/a.jpg"}}}`)
	f.set("userInfo.VipQueryServer.SRFVipQuery_V2", `json:{"code":0,"data":{"infoMap":{"12345":{"vipFlag":0}}}}`)
	r, err := p.PollQR(ctx, ch.Key)
	if err != nil || r.Status != online.QRConfirmed || r.Account == nil || r.Account.Nickname != "阿伦" || r.Account.VIP {
		t.Fatalf("confirm %+v %v", r, err)
	}
	if c.Cookie(kUin) != "12345" || c.Cookie(kMusicKey) != "Q_H_L_KEY" || c.Cookie(kRefreshKey) != "RK" {
		t.Fatalf("session %v", c.Cookies())
	}
	for k := range c.Cookies() {
		if k == "p_skey" || k == "qrsig" {
			t.Fatalf("transient ptlogin cookie leaked into jar: %s", k)
		}
	}
	// logged-in musicu calls carry uin/authst
	p.Charts(ctx)
	f.mu.Lock()
	var comm map[string]any
	json.Unmarshal(f.bodies[len(f.bodies)-1]["comm"], &comm)
	ck := f.cookies[len(f.cookies)-1]
	f.mu.Unlock()
	if comm["uin"] != "12345" || comm["authst"] != "Q_H_L_KEY" || !strings.Contains(ck, "qm_keyst=Q_H_L_KEY") {
		t.Fatalf("comm %v cookie %q", comm, ck)
	}
	f.set("music.login.LoginServer.Login", `json:{"code":0,"data":{"musicid":12345,"musickey":"NEWKEY"}}`)
	if err := p.Refresh(ctx); err != nil || c.Cookie(kMusicKey) != "NEWKEY" {
		t.Fatalf("refresh %v", err)
	}
	f.set("music.musicasset.PlaylistBaseRead.GetPlaylistByUin", `json:{"code":0,"data":{"v_playlist":[{"tid":1,"dirId":201,"dirName":"我喜欢","songNum":9},{"tid":2,"dirId":5,"dirName":"Road","songNum":1}]}}`)
	ups, err := p.UserPlaylists(ctx)
	if err != nil || len(ups) != 2 || ups[0].Kind != "liked" || ups[1].Name != "Road" {
		t.Fatalf("user playlists %+v %v", ups, err)
	}
}

func TestCookieLogin(t *testing.T) {
	p, f, c := newPlugin(t)
	ctx := context.Background()
	if _, err := p.LoginCookie(ctx, "foo=1"); !errors.Is(err, online.ErrBadInput) {
		t.Fatal(err)
	}
	f.set("music.UserInfo.userInfoServer.GetLoginUserInfo", `json:{"code":1000,"data":{}}`)
	f.set("userInfo.VipQueryServer.SRFVipQuery_V2", `json:{"code":1000,"data":{}}`)
	if _, err := p.LoginCookie(ctx, "uin=o0012345; qm_keyst=EXPIRED"); !errors.Is(err, online.ErrNeedsLogin) {
		t.Fatalf("expired %v", err)
	}
	if p.loggedIn() {
		t.Fatal("failed login left a session")
	}
	f.set("music.UserInfo.userInfoServer.GetLoginUserInfo", `json:{"code":0,"data":{"nick":"N"}}`)
	f.set("userInfo.VipQueryServer.SRFVipQuery_V2", `json:{"code":0,"data":{"infoMap":{"12345":{"vipFlag":1}}}}`)
	acc, err := p.LoginCookie(ctx, "uin=o0012345; qm_keyst=GOOD")
	if err != nil || !acc.VIP || c.Cookie(kUin) != "12345" {
		t.Fatalf("cookie %+v %v", acc, err)
	}
	// VIP sees paid songs as fully playable
	res, _ := p.Search(ctx, "x", 5)
	if res.Tracks[0].Playable != online.EntFull {
		t.Fatalf("%+v", res.Tracks[0])
	}
	if _, err := p.LoginPassword(ctx, "a", "b"); !errors.Is(err, online.ErrNotSupported) {
		t.Fatal(err)
	}
}

func TestHotSearch(t *testing.T) {
	p, _, _ := newPlugin(t)
	items, err := p.HotSearch(context.Background())
	if err != nil {
		t.Fatalf("hot %v", err)
	}
	// duplicate 周杰伦 and empty query stripped
	if len(items) != 2 || items[0].Keyword != "neon" || items[0].Score != 1217950 || items[1].Keyword != "天龙八部之宿敌" || items[1].Score != 720124 {
		t.Fatalf("items %+v", items)
	}
	if items[1].URL == "" {
		t.Fatalf("expected jump_url %+v", items[1])
	}
}

func TestArtistAlbums(t *testing.T) {
	p, _, _ := newPlugin(t)
	page, err := p.ArtistAlbums(context.Background(), "artist-0025NhlN2yWrP4", 0, 30)
	if err != nil {
		t.Fatalf("albums %v", err)
	}
	if page.Total != 2 || len(page.Items) != 2 || page.Items[0].ID != "qq:album-000MkMni19ClKG" || page.Items[0].Title != "叶惠美" || page.Items[0].Year != 2003 || page.Items[0].TrackCount != 11 {
		t.Fatalf("page %+v", page)
	}
	if page.Items[0].ArtistName != "周杰伦" || !strings.HasPrefix(page.Items[0].CoverURL, "https://y.gtimg.cn/") {
		t.Fatalf("cover/artist %+v", page.Items[0])
	}
}
