// Package kugou is the 酷狗音乐 plugin: a native Go client for Kugou's Android
// gateway protocol (signed requests, server-registered device id). It plays
// only what the account is entitled to — the full track, or Kugou's official
// preview clip for VIP songs on non-VIP accounts — and never decrypts kgm,
// unblocks, or borrows URLs from other platforms. Protocol knowledge from
// KuGouMusicApi (MIT, MakcRe); the implementation is our own.
package kugou

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/my-name-is-alan/yimeng/core/internal/lyrics"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/online"
)

type Plugin struct {
	api
	cfg online.Config
}

// New returns a factory; zero Options use the production hosts.
func New(o Options) online.Factory {
	return func(env online.Env) (online.Plugin, error) {
		return &Plugin{api: api{c: env.HTTP, opts: o, now: time.Now}, cfg: env.Config}, nil
	}
}

func (p *Plugin) Manifest() online.Manifest {
	return online.Manifest{
		ID: origin, Name: "酷狗音乐", Version: "1.0.0", Homepage: "https://www.kugou.com",
		Description: "搜索、歌词、排行榜、每日推荐、歌单广场、我的歌单与播放（按账号权益）。",
		Capabilities: []online.Capability{online.CapSearch, online.CapLyrics, online.CapCharts, online.CapRecommend,
			online.CapDiscover, online.CapUserPlaylists, online.CapStream},
		LoginMethods: []online.LoginMethod{online.LoginQR, online.LoginSMS, online.LoginCookie},
		QRChannels:   []online.QRChannel{{ID: "app", Label: "酷狗 App 扫码", App: "酷狗音乐 App", Hint: "请使用 酷狗音乐 App 扫码（我的 → 右上角扫一扫）"}},
		Qualities:    []online.Quality{online.QStandard, online.QHigh, online.QLossless, online.QHiRes},
		DefaultRate:  30,
		Notice:       "仅播放当前账号有权收听的内容：非会员播放会员歌曲时只提供酷狗官方试听片段（通常 60 秒）。不解密 kgm，不绕过任何限制。",
		Credits:      []string{"KuGouMusicApi (MIT, MakcRe) — protocol reference"},
	}
}

func (p *Plugin) vip() bool { v := p.c.Cookie(kVIP); return p.loggedIn() && v != "" && v != "0" }

func (p *Plugin) device(ctx context.Context) {
	if err := p.registerDevice(ctx); err != nil && p.c.Cookie(kDfid) == "" {
		// keep going with dfid "-": read-only endpoints accept it
		_ = err
	}
}

// ── auth ───────────────────────────────────────────────────────────────────

const qrPage = "https://h5.kugou.com/apps/loginQRCode/html/index.html"

func (p *Plugin) StartQR(ctx context.Context) (online.QRChallenge, error) {
	p.device(ctx)
	var r struct {
		Data struct {
			Qrcode string `json:"qrcode"`
		} `json:"data"`
	}
	err := p.call(ctx, req{base: hLoginQR, path: "/v2/qrcode", sig: sigWeb, params: map[string]string{
		"appid": "1001", "type": "1", "plat": "4", "qrcode_txt": qrPage + "?appid=" + appID + "&", "srcappid": srcAppID}}, &r)
	if err != nil {
		return online.QRChallenge{}, err
	}
	if r.Data.Qrcode == "" {
		return online.QRChallenge{}, fmt.Errorf("%w: empty qr key", online.ErrUpstream)
	}
	return online.QRChallenge{Key: r.Data.Qrcode, Content: qrPage + "?qrcode=" + r.Data.Qrcode, ExpiresAt: p.now().Add(3 * time.Minute)}, nil
}

func (p *Plugin) PollQR(ctx context.Context, key string) (online.QRPoll, error) {
	var r struct {
		Data struct {
			Status   int     `json:"status"`
			Nickname string  `json:"nickname"`
			Pic      string  `json:"pic"`
			Token    string  `json:"token"`
			UserID   flexStr `json:"userid"`
		} `json:"data"`
	}
	err := p.call(ctx, req{base: hLoginQR, path: "/v2/get_userinfo_qrcode", sig: sigWeb, params: map[string]string{
		"plat": "4", "appid": appID, "srcappid": srcAppID, "qrcode": key}}, &r)
	if err != nil {
		return online.QRPoll{}, err
	}
	switch r.Data.Status {
	case 1:
		return online.QRPoll{Status: online.QRWaiting}, nil
	case 2:
		return online.QRPoll{Status: online.QRScanned, Nickname: r.Data.Nickname}, nil
	case 4:
		if r.Data.Token == "" || r.Data.UserID == "" {
			return online.QRPoll{}, fmt.Errorf("%w: qr confirmed without token", online.ErrUpstream)
		}
		p.setSession(r.Data.Token, string(r.Data.UserID), "")
		acc, err := p.Account(ctx)
		if err != nil {
			acc = online.Account{UserID: string(r.Data.UserID), Nickname: r.Data.Nickname, Avatar: img(r.Data.Pic, 200)}
		}
		return online.QRPoll{Status: online.QRConfirmed, Nickname: acc.Nickname, Account: &acc}, nil
	default: // 0 = expired
		return online.QRPoll{Status: online.QRExpired}, nil
	}
}

func (p *Plugin) setSession(token, userid, vip string) {
	keep := map[string]string{}
	for k, v := range p.c.Cookies() {
		if strings.HasPrefix(k, online.DevicePrefix) {
			keep[k] = v
		}
	}
	keep[kToken], keep[kUserID] = token, userid
	if vip != "" {
		keep[kVIP] = vip
	}
	p.c.ReplaceCookies(keep)
}

// LoginCookie accepts "token=…; userid=…" or a kugou.com web cookie whose
// KuGoo value carries KugooID=…&t=….
func (p *Plugin) LoginCookie(ctx context.Context, cookie string) (online.Account, error) {
	m := online.ParseCookieString(cookie)
	token, uid := m["token"], m["userid"]
	if kg := m["KuGoo"]; kg != "" && (token == "" || uid == "") {
		for _, part := range strings.Split(kg, "&") {
			k, v, _ := strings.Cut(part, "=")
			switch k {
			case "KugooID":
				uid = v
			case "t":
				token = v
			}
		}
	}
	if token == "" || uid == "" {
		return online.Account{}, fmt.Errorf("%w: cookie must contain token and userid (or KuGoo=KugooID…&t…)", online.ErrBadInput)
	}
	prev := p.c.Cookies()
	p.setSession(token, uid, m["vip_type"])
	acc, err := p.Account(ctx)
	if err != nil {
		p.c.ReplaceCookies(prev)
		return online.Account{}, err
	}
	return acc, nil
}

func (p *Plugin) LoginPassword(ctx context.Context, user, pass string) (online.Account, error) {
	return online.Account{}, fmt.Errorf("%w: 酷狗账号密码登录需要 App 内验证，请使用扫码或短信验证码登录", online.ErrNotSupported)
}

func (p *Plugin) SendSMS(ctx context.Context, phone string) error {
	p.device(ctx)
	return p.call(ctx, req{method: http.MethodPost, base: hLogin, path: "/v7/send_mobile_code",
		body: map[string]any{"businessid": 5, "mobile": phone, "plat": 3}}, nil)
}

func maskPhone(s string) string {
	if len(s) < 11 {
		return s
	}
	return s[:2] + "*****" + s[10:11]
}

func (p *Plugin) LoginSMS(ctx context.Context, phone, code string) (online.Account, error) {
	p.device(ctx)
	ms := p.now().UnixMilli()
	plain, _ := json.Marshal(map[string]string{"mobile": phone, "code": code})
	enc, temp := aesTemp(plain)
	pk, _ := json.Marshal(map[string]any{"clienttime_ms": ms, "key": temp})
	body := map[string]any{
		"plat": 1, "support_multi": 1, "t1": 0, "t2": 0, "t3": "MCwwLDAsMCwwLDAsMCwwLDA=",
		"clienttime_ms": ms, "mobile": maskPhone(phone), "key": signParamsKey(strconv.FormatInt(ms, 10)),
		"pk": strings.ToUpper(rsaRaw(pk)), "params": enc,
	}
	if err := p.loginResult(ctx, req{method: http.MethodPost, base: hLoginSMS, path: "/v7/login_by_verifycode", body: body,
		headers: map[string]string{"support-calm": "1"}}, temp); err != nil {
		return online.Account{}, err
	}
	return p.Account(ctx)
}

// loginResult handles login_by_verifycode / login_by_token responses: the
// token may come back AES-wrapped in secu_params under our temp key.
func (p *Plugin) loginResult(ctx context.Context, r req, temp string) error {
	var res struct {
		Data struct {
			Token      string  `json:"token"`
			UserID     flexStr `json:"userid"`
			VIPType    flexStr `json:"vip_type"`
			T1         string  `json:"t1"`
			SecuParams string  `json:"secu_params"`
		} `json:"data"`
	}
	if err := p.call(ctx, r, &res); err != nil {
		return err
	}
	d := res.Data
	if d.SecuParams != "" {
		if plain, err := aesTempDecrypt(d.SecuParams, temp); err == nil {
			var sp struct {
				Token string `json:"token"`
			}
			if json.Unmarshal(plain, &sp) == nil && sp.Token != "" {
				d.Token = sp.Token
			} else if !strings.HasPrefix(string(plain), "{") {
				d.Token = string(plain)
			}
		}
	}
	if d.Token == "" {
		return fmt.Errorf("%w: login returned no token", online.ErrUpstream)
	}
	uid := string(d.UserID)
	if uid == "" {
		uid = p.c.Cookie(kUserID)
	}
	p.setSession(d.Token, uid, string(d.VIPType))
	if d.T1 != "" {
		p.c.SetCookie(kT1, d.T1)
	}
	return nil
}

func (p *Plugin) Account(ctx context.Context) (online.Account, error) {
	if !p.loggedIn() {
		return online.Account{}, online.ErrNeedsLogin
	}
	ct := p.now().Unix()
	pk, _ := json.Marshal(map[string]any{"token": p.c.Cookie(kToken), "clienttime": ct})
	uid := p.c.Cookie(kUserID)
	var r struct {
		Data struct {
			Nickname string  `json:"nickname"`
			Pic      string  `json:"pic"`
			UserID   flexStr `json:"userid"`
			VIPType  flexInt `json:"vip_type"`
		} `json:"data"`
	}
	n, _ := strconv.ParseInt(uid, 10, 64)
	if err := p.call(ctx, req{method: http.MethodPost, path: "/v3/get_my_info", router: "usercenter.kugou.com",
		params: map[string]string{"plat": "1"},
		body:   map[string]any{"visit_time": ct, "usertype": 1, "p": strings.ToUpper(rsaRaw(pk)), "userid": n}}, &r); err != nil {
		return online.Account{}, err
	}
	acc := online.Account{UserID: uid, Nickname: r.Data.Nickname, Avatar: img(r.Data.Pic, 200)}
	vip := r.Data.VIPType > 0
	var v struct {
		Data struct {
			IsVIP   flexInt `json:"is_vip"`
			VIPType flexInt `json:"vip_type"`
		} `json:"data"`
	}
	if p.call(ctx, req{base: hVIP, path: "/v1/get_union_vip", params: map[string]string{"busi_type": "concept"}}, &v) == nil {
		vip = vip || v.Data.IsVIP > 0 || v.Data.VIPType > 0
	}
	if vip {
		p.c.SetCookie(kVIP, "1")
		acc.VIP, acc.Tier = true, "VIP"
	} else {
		p.c.SetCookie(kVIP, "0")
	}
	if acc.Nickname == "" {
		acc.Nickname = "酷狗用户 " + uid
	}
	return acc, nil
}

// Refresh renews the token (login_by_token).
func (p *Plugin) Refresh(ctx context.Context) error {
	if !p.loggedIn() {
		return online.ErrNeedsLogin
	}
	ms := p.now().UnixMilli()
	p3plain, _ := json.Marshal(map[string]any{"clienttime": ms / 1000, "token": p.c.Cookie(kToken)})
	p3 := fmt.Sprintf("%x", aesCBC(p3plain, tokenKey, tokenIV))
	enc, temp := aesTemp([]byte("{}"))
	pk, _ := json.Marshal(map[string]any{"clienttime_ms": ms, "key": temp})
	body := map[string]any{"dfid": p.dfid(), "p3": p3, "plat": 1, "t1": 0, "t2": 0, "t3": "MCwwLDAsMCwwLDAsMCwwLDA=",
		"pk": rsaRaw(pk), "params": enc, "userid": p.c.Cookie(kUserID), "clienttime_ms": ms}
	return p.loginResult(ctx, req{method: http.MethodPost, base: hLogin, path: "/v5/login_by_token", body: body}, temp)
}

// ── catalog ────────────────────────────────────────────────────────────────

func (p *Plugin) search(ctx context.Context, kind, q string, n int) (obj, error) {
	var params map[string]string
	path := "/v1/search/" + kind
	switch kind {
	case "song":
		path = "/v2/search/song"
		params = map[string]string{"keyword": q, "page": "1", "pagesize": strconv.Itoa(n), "platform": "AndroidFilter",
			"iscorrection": "1", "privilegefilter": "0", "area_code": "1", "dopicfull": "1"}
	case "album":
		params = map[string]string{"keyword": q, "page": "1", "pagesize": strconv.Itoa(n), "platform": "AndroidFilter",
			"iscorrection": "1", "category": "1", "sorttype": "0", "searchsong": "0"}
	default:
		params = map[string]string{"albumhide": "0", "iscorrection": "1", "keyword": q, "nocollect": "0",
			"page": "1", "pagesize": strconv.Itoa(n), "platform": "AndroidFilter"}
	}
	var r obj
	err := p.call(ctx, req{path: path, router: "complexsearch.kugou.com", params: params}, &r)
	return r, err
}

func (p *Plugin) Search(ctx context.Context, q string, limit int) (online.SearchResult, error) {
	if limit <= 0 || limit > 50 {
		limit = 30
	}
	p.device(ctx)
	res := online.SearchResult{}
	r, err := p.search(ctx, "song", q, limit)
	if err != nil {
		return res, err
	}
	res.Tracks = mapSongs(list(r, "data.lists"), p.vip())
	small := min(limit, 10)
	if r, err := p.search(ctx, "album", q, small); err == nil {
		for _, a := range list(r, "data.lists") {
			res.Albums = append(res.Albums, mapAlbum(a))
		}
	}
	if r, err := p.search(ctx, "author", q, small); err == nil {
		for _, a := range list(r, "data.lists") {
			res.Artists = append(res.Artists, mapArtist(a))
		}
	}
	if r, err := p.search(ctx, "special", q, small); err == nil {
		for _, s := range list(r, "data.lists") {
			res.Playlists = append(res.Playlists, mapPlaylist(s))
		}
	}
	return res, nil
}

func mapAlbum(a obj) online.Album {
	id := str(a, "albumid", "album_id")
	arts := artistRefs(a, str(a, "singer", "author_name"))
	al := online.Album{ID: fid("album-" + id), Origin: origin, Title: str(a, "albumname", "album_name"), Artists: arts, ArtistName: joinNames(arts),
		CoverURL: img(str(a, "sizable_cover", "img"), 480), TrackCount: int(num(a, "songcount")), Description: str(a, "intro")}
	if d := str(a, "publish_time", "publish_date"); len(d) >= 4 {
		al.Year, _ = strconv.Atoi(d[:4])
	}
	return al
}

func mapArtist(a obj) online.Artist {
	id := str(a, "AuthorId", "author_id")
	return online.Artist{ID: fid("artist-" + id), Origin: origin, Name: str(a, "AuthorName", "author_name"),
		PictureURL: img(str(a, "sizable_avatar", "Avatar"), 480), AlbumCount: int(num(a, "AlbumCount", "album_count")),
		TrackCount: int(num(a, "AudioCount", "song_count")), Description: str(a, "intro")}
}

// privileges fetches track info + per-quality rights for hashes.
func (p *Plugin) privileges(ctx context.Context, refs []trackRef) ([]obj, error) {
	res := make([]map[string]any, len(refs))
	for i, r := range refs {
		al, _ := strconv.ParseInt(r.Album, 10, 64)
		res[i] = map[string]any{"type": "audio", "page_id": 0, "hash": r.Hash, "album_id": al}
	}
	var out obj
	err := p.call(ctx, req{method: http.MethodPost, path: "/v2/get_res_privilege/lite", router: "media.store.kugou.com",
		body: map[string]any{"appid": 1005, "area_code": 1, "behavior": "play", "clientver": 20489, "need_hash_offset": 1, "relate": 1,
			"support_verify": 1, "resource": res, "qualities": []string{"128", "320", "flac", "high", "super"}}}, &out)
	return list(out, "data"), err
}

func (p *Plugin) Tracks(ctx context.Context, ids []string) ([]online.Track, error) {
	var out []online.Track
	for start := 0; start < len(ids); start += 50 {
		end := min(start+50, len(ids))
		refs := make([]trackRef, 0, end-start)
		for _, id := range ids[start:end] {
			r, err := parseTrack(id)
			if err != nil {
				return nil, err
			}
			refs = append(refs, r)
		}
		items, err := p.privileges(ctx, refs)
		if err != nil {
			return nil, err
		}
		for i, it := range items {
			// keep the caller's album/mix ids when the response lacks them
			if str(it, "album_id") == "" && i < len(refs) {
				it["album_id"] = refs[i].Album
			}
			for _, g := range list(it, "relate_goods") {
				switch str(g, "quality") {
				case "320":
					it["hash_320"] = str(g, "hash")
				case "flac":
					it["hash_flac"] = str(g, "hash")
				case "high":
					it["hash_high"] = str(g, "hash")
				}
			}
			if t, ok := mapSong(it, p.vip()); ok {
				out = append(out, t)
			}
		}
	}
	return out, nil
}

func trimKind(id, kind string) string { return strings.TrimPrefix(id, kind+"-") }

func (p *Plugin) Album(ctx context.Context, id string) (online.Album, error) {
	id = trimKind(id, "album")
	var d obj
	if err := p.call(ctx, req{method: http.MethodPost, path: "/kmr/v2/albums", router: "openapi.kugou.com", headers: map[string]string{"kg-tid": "255"},
		body: map[string]any{"data": []map[string]any{{"album_id": id}}, "is_buy": 0,
			"fields": "album_id,album_name,publish_date,sizable_cover,intro,language,is_publish,heat,type,quality,authors,exclusive,author_name,trans_param"}}, &d); err != nil {
		return online.Album{}, err
	}
	als := list(d, "data")
	if len(als) == 0 || str(als[0], "album_name") == "" {
		return online.Album{}, fmt.Errorf("%w: album %s", online.ErrUnavailable, id)
	}
	al := mapAlbum(als[0])
	var songs []obj
	for page := 1; page <= 10; page++ {
		var s obj
		if err := p.call(ctx, req{method: http.MethodPost, path: "/v1/album_audio/lite", router: "openapi.kugou.com", headers: map[string]string{"kg-tid": "255"},
			body: map[string]any{"album_id": id, "is_buy": "", "page": page, "pagesize": 50}}, &s); err != nil {
			return al, err
		}
		got := list(s, "data.songs")
		songs = append(songs, got...)
		if len(got) < 50 || len(songs) >= int(num(s, "data.total")) {
			break
		}
	}
	al.Tracks = mapSongs(songs, p.vip())
	for i := range al.Tracks {
		if al.Tracks[i].CoverURL == "" {
			al.Tracks[i].CoverURL = al.CoverURL
		}
	}
	al.TrackCount = len(al.Tracks)
	return al, nil
}

func (p *Plugin) Artist(ctx context.Context, id string) (online.Artist, error) {
	id = trimKind(id, "artist")
	var d obj
	if err := p.call(ctx, req{method: http.MethodPost, path: "/kmr/v3/author", router: "openapi.kugou.com", headers: map[string]string{"kg-tid": "36"},
		body: map[string]any{"author_id": id}}, &d); err != nil {
		return online.Artist{}, err
	}
	data, _ := get(d, "data").(map[string]any)
	if data == nil {
		return online.Artist{}, fmt.Errorf("%w: artist %s", online.ErrUnavailable, id)
	}
	ar := mapArtist(data)
	ct := p.now().Unix()
	var s obj
	if err := p.call(ctx, req{method: http.MethodPost, base: "https://openapi.kugou.com", path: "/kmr/v1/audio_group/author", router: "openapi.kugou.com",
		headers: map[string]string{"kg-tid": "220"},
		body: map[string]any{"appid": appID, "clientver": clientVer, "mid": p.c.Cookie(kMid), "clienttime": ct, "key": signParamsKey(strconv.FormatInt(ct, 10)),
			"author_id": id, "pagesize": 50, "page": 1, "sort": 1, "area_code": "all"}}, &s); err == nil {
		ar.TopTracks = mapSongs(list(s, "data"), p.vip())
	}
	return ar, nil
}

func (p *Plugin) Playlist(ctx context.Context, id string) (online.Playlist, error) {
	switch {
	case strings.HasPrefix(id, "rank-"):
		return p.rank(ctx, trimKind(id, "rank"))
	case strings.HasPrefix(id, "pl-"):
		return p.collection(ctx, trimKind(id, "pl"))
	}
	return online.Playlist{}, fmt.Errorf("%w: kugou playlist id %q", online.ErrBadInput, id)
}

func (p *Plugin) collection(ctx context.Context, gid string) (online.Playlist, error) {
	var d obj
	if err := p.call(ctx, req{method: http.MethodPost, path: "/v3/get_list_info", router: "pubsongs.kugou.com",
		body: map[string]any{"data": []map[string]string{{"global_collection_id": gid}}, "userid": p.c.Cookie(kUserID), "token": p.c.Cookie(kToken)}}, &d); err != nil {
		return online.Playlist{}, err
	}
	info := list(d, "data")
	if len(info) == 0 {
		return online.Playlist{}, fmt.Errorf("%w: playlist %s", online.ErrUnavailable, gid)
	}
	pl := mapPlaylist(info[0])
	pl.ID = fid("pl-" + gid)
	pl.Mine = p.loggedIn() && str(info[0], "list_create_userid") == p.c.Cookie(kUserID)
	var songs []obj
	const size = 300
	for page := 0; page < 10; page++ {
		var s obj
		if err := p.call(ctx, req{path: "/pubsongs/v2/get_other_list_file_nofilt", params: map[string]string{
			"area_code": "1", "begin_idx": strconv.Itoa(page * size), "plat": "1", "type": "1", "mode": "1", "personal_switch": "1",
			"extend_fields": "abtags,hot_cmt,popularization", "pagesize": strconv.Itoa(size), "global_collection_id": gid}}, &s); err != nil {
			if page == 0 {
				return pl, err
			}
			break
		}
		got := list(s, "data.songs")
		songs = append(songs, got...)
		if len(got) < size || len(songs) >= int(num(s, "data.count")) {
			break
		}
	}
	pl.Tracks = mapSongs(songs, p.vip())
	if pl.TrackCount < len(pl.Tracks) {
		pl.TrackCount = len(pl.Tracks)
	}
	return pl, nil
}

func (p *Plugin) rankList(ctx context.Context) ([]obj, error) {
	var d obj
	if err := p.call(ctx, req{path: "/ocean/v6/rank/list", params: map[string]string{"plat": "2", "withsong": "0", "parentid": "0"}}, &d); err != nil {
		return nil, err
	}
	return list(d, "data.info"), nil
}

func mapRank(r obj) online.Playlist {
	return online.Playlist{ID: fid("rank-" + str(r, "rankid")), Origin: origin, Kind: "chart", Name: str(r, "rankname"),
		CoverURL: img(str(r, "imgurl", "img_9", "album_img_9", "banner_9"), 400), Description: str(r, "intro"),
		Subtitle: str(r, "update_frequency"), PlayCount: num(r, "play_times")}
}

func (p *Plugin) rank(ctx context.Context, rid string) (online.Playlist, error) {
	pl := online.Playlist{ID: fid("rank-" + rid), Origin: origin, Kind: "chart", Name: "酷狗排行榜"}
	if rs, err := p.rankList(ctx); err == nil {
		for _, r := range rs {
			if str(r, "rankid") == rid {
				pl = mapRank(r)
			}
		}
	}
	var songs []obj
	for page := 1; page <= 5; page++ {
		var s obj
		if err := p.call(ctx, req{method: http.MethodPost, path: "/openapi/kmr/v2/rank/audio", headers: map[string]string{"kg-tid": "369"},
			body: map[string]any{"show_portrait_mv": 1, "show_type_total": 1, "filter_original_remarks": 1, "area_code": 1,
				"pagesize": 100, "rank_cid": 0, "type": 1, "page": page, "rank_id": rid}}, &s); err != nil {
			if page == 1 {
				return pl, err
			}
			break
		}
		got := list(s, "data.songlist")
		songs = append(songs, got...)
		if len(got) < 100 || len(songs) >= int(num(s, "data.total")) {
			break
		}
	}
	pl.Tracks = mapSongs(songs, p.vip())
	pl.TrackCount = len(pl.Tracks)
	return pl, nil
}

// Lyrics: plain LRC only (fmt=lrc, base64 text). The encrypted KRC format is
// never requested or decoded.
func (p *Plugin) Lyrics(ctx context.Context, id string) (online.Lyrics, error) {
	ref, err := parseTrack(id)
	if err != nil {
		return online.Lyrics{}, err
	}
	var s obj
	if err := p.call(ctx, req{base: hLyrics, path: "/v1/search", noDefault: true, params: map[string]string{
		"album_audio_id": ref.Mix, "appid": appID, "clientver": clientVer, "duration": "0", "hash": ref.Hash,
		"keyword": "", "lrctxt": "1", "man": "no"}}, &s); err != nil {
		return online.Lyrics{}, err
	}
	cands := list(s, "candidates")
	if len(cands) == 0 {
		return online.Lyrics{}, fmt.Errorf("%w: no lyrics", online.ErrUnavailable)
	}
	cand := cands[0]
	// KRC first (word timing + translation/roma), plain LRC as fallback.
	var k struct {
		Content string `json:"content"`
	}
	if err := p.call(ctx, req{base: hLyrics, path: "/download", params: map[string]string{"ver": "1", "client": "android",
		"id": str(cand, "id"), "accesskey": str(cand, "accesskey"), "fmt": "krc", "charset": "utf8"}}, &k); err == nil && k.Content != "" {
		if txt, err := lyrics.DecodeKRC(k.Content); err == nil {
			if ws := lyrics.ParseKRCText(txt); len(ws) > 0 {
				l := online.Lyrics{Synced: true, LRC: lyrics.WordsToLRC(ws), Words: ws}
				tr, ro := lyrics.KRCExtras(txt)
				l.Translation, l.Roma = alignedLRC(ws, tr), alignedLRC(ws, ro)
				return l, nil
			}
		}
	}
	var d struct {
		Content string `json:"content"`
	}
	if err := p.call(ctx, req{base: hLyrics, path: "/download", params: map[string]string{"ver": "1", "client": "android",
		"id": str(cand, "id"), "accesskey": str(cand, "accesskey"), "fmt": "lrc", "charset": "utf8"}}, &d); err != nil {
		return online.Lyrics{}, err
	}
	b, err := base64.StdEncoding.DecodeString(d.Content)
	if err != nil || len(b) == 0 {
		return online.Lyrics{}, fmt.Errorf("%w: empty lyrics", online.ErrUnavailable)
	}
	lrc := strings.ReplaceAll(strings.TrimPrefix(string(b), "\ufeff"), "\r\n", "\n")
	return online.Lyrics{Synced: strings.Contains(lrc, "[0"), LRC: lrc}, nil
}

// alignedLRC renders KRC per-line extras (index-aligned) as LRC.
func alignedLRC(ws []lyrics.WordLine, vals []string) string {
	if len(vals) == 0 {
		return ""
	}
	var out []lyrics.WordLine
	for i, w := range ws {
		if i < len(vals) && strings.TrimSpace(vals[i]) != "" {
			out = append(out, lyrics.WordLine{TMs: w.TMs, Text: vals[i]})
		}
	}
	return lyrics.WordsToLRC(out)
}

// ── discovery ──────────────────────────────────────────────────────────────

func (p *Plugin) Charts(ctx context.Context) ([]online.Playlist, error) {
	rs, err := p.rankList(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]online.Playlist, 0, len(rs))
	for _, r := range rs {
		if str(r, "rankid") != "" {
			out = append(out, mapRank(r))
		}
	}
	return out, nil
}

func (p *Plugin) RecommendTracks(ctx context.Context) ([]online.Track, error) {
	p.device(ctx)
	uid := p.c.Cookie(kUserID)
	if uid == "" {
		uid = "0"
	}
	var d obj
	if err := p.call(ctx, req{method: http.MethodPost, path: "/everyday_song_recommend", router: "everydayrec.service.kugou.com",
		body: map[string]any{"platform": "android", "userid": uid}}, &d); err != nil {
		return nil, err
	}
	return mapSongs(list(d, "data.song_list"), p.vip()), nil
}

func (p *Plugin) specials(ctx context.Context, cat string, page, size int) ([]online.Playlist, error) {
	ct := p.now().Unix()
	uid, _ := strconv.ParseInt(p.c.Cookie(kUserID), 10, 64)
	catID, _ := strconv.Atoi(cat)
	var d obj
	if err := p.call(ctx, req{method: http.MethodPost, path: "/v2/special_recommend", router: "specialrec.service.kugou.com",
		body: map[string]any{"appid": appID, "mid": p.c.Cookie(kMid), "clientver": clientVer, "platform": "android", "clienttime": ct,
			"userid": uid, "module_id": 1, "page": page, "pagesize": size, "key": signParamsKey(strconv.FormatInt(ct, 10)),
			"special_recommend": map[string]any{"withtag": 1, "withsong": 1, "sort": 1, "ugc": 1, "is_selected": 0, "withrecommend": 1, "area_code": 1, "categoryid": catID},
			"req_multi":         1, "retrun_min": 5, "return_special_falg": 1}}, &d); err != nil {
		return nil, err
	}
	var out []online.Playlist
	for _, s := range list(d, "data.special_list") {
		if str(s, "global_collection_id") != "" {
			out = append(out, mapPlaylist(s))
		}
	}
	return out, nil
}

func (p *Plugin) RecommendPlaylists(ctx context.Context) ([]online.Playlist, error) {
	return p.specials(ctx, "0", 1, 30)
}

func (p *Plugin) DiscoverPlaylists(ctx context.Context, cat string, offset, limit int) ([]online.Playlist, error) {
	if limit <= 0 || limit > 50 {
		limit = 30
	}
	return p.specials(ctx, cat, offset/limit+1, limit)
}

func (p *Plugin) UserPlaylists(ctx context.Context) ([]online.Playlist, error) {
	if !p.loggedIn() {
		return nil, online.ErrNeedsLogin
	}
	uid, tok := p.c.Cookie(kUserID), p.c.Cookie(kToken)
	n, _ := strconv.ParseInt(uid, 10, 64)
	var d obj
	if err := p.call(ctx, req{method: http.MethodPost, path: "/v7/get_all_list", router: "cloudlist.service.kugou.com",
		params: map[string]string{"plat": "1", "userid": uid, "token": tok},
		body:   map[string]any{"userid": n, "token": tok, "total_ver": 979, "type": 2, "page": 1, "pagesize": 100}}, &d); err != nil {
		return nil, err
	}
	var out []online.Playlist
	for _, it := range list(d, "data.info") {
		if str(it, "global_collection_id") == "" {
			continue
		}
		pl := mapPlaylist(it)
		pl.Mine = str(it, "list_create_userid") == uid
		if pl.Name == "我喜欢" || num(it, "is_def") == 2 {
			pl.Kind = "liked"
		}
		out = append(out, pl)
	}
	return out, nil
}

// ── stream ─────────────────────────────────────────────────────────────────

var qualityParam = map[online.Quality]string{online.QStandard: "128", online.QHigh: "320", online.QLossless: "flac", online.QHiRes: "high"}
var qualityOf = map[string]online.Quality{"128": online.QStandard, "320": online.QHigh, "flac": online.QLossless, "high": online.QHiRes}

type urlResp struct {
	Status     int      `json:"status"`
	URL        []string `json:"url"`
	BackupURL  []string `json:"backupUrl"`
	TimeLength int64    `json:"timeLength"`
	FileSize   int64    `json:"fileSize"`
	BitRate    int      `json:"bitRate"`
	ExtName    string   `json:"extName"`
	HashOffset struct {
		StartMs int64 `json:"start_ms"`
		EndMs   int64 `json:"end_ms"`
	} `json:"hash_offset"`
	FailProcess any `json:"fail_process"`
}

func (p *Plugin) songURL(ctx context.Context, ref trackRef, quality string, freePart bool) (urlResp, error) {
	fp := "0"
	if freePart {
		fp = "1"
	}
	var r urlResp
	body, _, err := p.raw(ctx, req{path: "/v5/url", router: "trackercdn.kugou.com", songKey: ref.Hash, params: map[string]string{
		"album_id": ref.Album, "area_code": "1", "hash": ref.Hash, "ssa_flag": "is_fromtrack", "version": "11430", "page_id": "151369488",
		"quality": quality, "album_audio_id": ref.Mix, "behavior": "play", "pid": "2", "cmd": "26", "pidversion": "3001", "IsFreePart": fp,
		"ppage_id": "463467626,350369493,788954147", "cdnBackup": "1", "module": "", "clientver": "11430"}})
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return r, fmt.Errorf("%w: song url: %v", online.ErrUpstream, err)
	}
	return r, nil
}

func pickURL(r urlResp) string {
	for _, u := range append(append([]string{}, r.URL...), r.BackupURL...) {
		if u != "" {
			return u
		}
	}
	return ""
}

// Resolve returns the full track when the account is entitled to it (trying
// the requested quality, then 128k), otherwise Kugou's official preview clip.
func (p *Plugin) Resolve(ctx context.Context, id string, q online.Quality) (online.Stream, error) {
	ref, err := parseTrack(id)
	if err != nil {
		return online.Stream{}, err
	}
	p.device(ctx)
	tries := []string{"128"}
	if qp := qualityParam[q]; qp != "" && qp != "128" {
		tries = []string{qp, "128"}
	}
	for _, qp := range tries {
		r, err := p.songURL(ctx, ref, qp, false)
		if err != nil {
			return online.Stream{}, err
		}
		if u := pickURL(r); r.Status == 1 && u != "" {
			if strings.Contains(u, "/yp/p_") { // clip path: never present a clip as the full song
				break
			}
			return p.stream(r, u, online.EntFull, qp), nil
		}
	}
	r, err := p.songURL(ctx, ref, "128", true)
	if err != nil {
		return online.Stream{}, err
	}
	if u := pickURL(r); r.Status == 1 && u != "" {
		s := p.stream(r, u, online.EntPreview, "128")
		s.PreviewStartMs, s.PreviewEndMs = r.HashOffset.StartMs, r.HashOffset.EndMs
		if s.PreviewEndMs == 0 {
			s.PreviewEndMs = 60000
		}
		s.Reason = "会员歌曲：当前账号仅可播放酷狗官方试听片段"
		return s, nil
	}
	return online.Stream{Entitlement: online.EntUnavailable, Reason: "酷狗未向当前账号提供该歌曲（版权或付费限制）"}, nil
}

func (p *Plugin) stream(r urlResp, u string, ent online.Entitlement, qp string) online.Stream {
	ext := r.ExtName
	if ext == "" {
		ext = "mp3"
	}
	return online.Stream{URL: u, Header: map[string]string{"User-Agent": userAgent}, Entitlement: ent, Quality: qualityOf[qp],
		Format: ext, BitrateKbps: r.BitRate / 1000, SizeBytes: r.FileSize, DurationMs: r.TimeLength * 1000,
		ExpiresAt: p.now().Add(20 * time.Minute)}
}
