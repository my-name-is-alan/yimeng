// Package netease is the 网易云音乐 plugin: native Go client for the weapi /
// eapi endpoints. It plays only what the logged-in account is entitled to
// (full, or the official trial segment for VIP/paid songs) and never uses
// unblocking or cross-platform URL matching.
package netease

import (
	"context"
	"fmt"
	"github.com/my-name-is-alan/yimeng/core/internal/lyrics"
	"os"
	"regexp"
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
	if o.Domain == "" {
		o.Domain = defaultDomain
	}
	if o.EapiDomain == "" {
		o.EapiDomain = defaultEapiDomain
	}
	return func(env online.Env) (online.Plugin, error) {
		return &Plugin{api: api{c: env.HTTP, opts: o}, cfg: env.Config}, nil
	}
}

func (p *Plugin) Manifest() online.Manifest {
	return online.Manifest{
		ID: origin, Name: "网易云音乐", Version: "1.0.0", Homepage: "https://music.163.com",
		Description: "搜索、歌词、排行榜、每日推荐、我的歌单与播放（按账号权益）。",
		Capabilities: []online.Capability{online.CapSearch, online.CapLyrics, online.CapCharts, online.CapRecommend,
			online.CapDiscover, online.CapUserPlaylists, online.CapStream, online.CapFM, online.CapHotSearch},
		LoginMethods: []online.LoginMethod{online.LoginQR, online.LoginCookie, online.LoginPassword, online.LoginSMS},
		QRChannels:   []online.QRChannel{{ID: "app", Label: "网易云 App 扫码", App: "网易云音乐 App", Hint: "请使用 网易云音乐 App 扫码（不是微信/QQ）"}},
		Qualities:    []online.Quality{online.QStandard, online.QHigh, online.QLossless, online.QHiRes},
		DefaultRate:  60,
		Notice:       "仅播放当前账号有权收听的内容：非会员播放会员歌曲时只提供官方试听片段。",
		Credits:      []string{"NeteaseCloudMusicApi Enhanced (MIT) — protocol reference"},
	}
}

// ── auth ───────────────────────────────────────────────────────────────────

func (p *Plugin) StartQR(ctx context.Context) (online.QRChallenge, error) {
	var r struct {
		Unikey string `json:"unikey"`
	}
	body, err := p.eapiRawAs(ctx, "/api/login/qrcode/unikey", map[string]any{"type": 3}, qrPC)
	if err == nil {
		err = decode(body, &r)
	}
	if err != nil {
		return online.QRChallenge{}, err
	}
	if r.Unikey == "" {
		return online.QRChallenge{}, fmt.Errorf("%w: empty qr key", online.ErrUpstream)
	}
	return online.QRChallenge{Key: r.Unikey, Content: "https://music.163.com/login?codekey=" + r.Unikey,
		ExpiresAt: time.Now().Add(5 * time.Minute)}, nil
}

func (p *Plugin) PollQR(ctx context.Context, key string) (online.QRPoll, error) {
	body, err := p.eapiRawAs(ctx, "/api/login/qrcode/client/login", map[string]any{"key": key, "type": 3}, qrPC)
	if err != nil {
		return online.QRPoll{}, err
	}
	var r struct {
		Code     int    `json:"code"`
		Message  string `json:"message"`
		Nickname string `json:"nickname"`
	}
	if err := jsonUnmarshal(body, &r); err != nil {
		return online.QRPoll{}, err
	}
	switch r.Code {
	case 800:
		return online.QRPoll{Status: online.QRExpired, Message: "二维码已过期"}, nil
	case 801:
		return online.QRPoll{Status: online.QRWaiting}, nil
	case 802:
		return online.QRPoll{Status: online.QRScanned, Nickname: r.Nickname, Message: "已扫码，请在手机上确认"}, nil
	case 803:
		if p.c.Cookie("MUSIC_U") == "" {
			return online.QRPoll{}, fmt.Errorf("%w: login confirmed but no session cookie", online.ErrUpstream)
		}
		acc, err := p.Account(ctx)
		if err != nil {
			return online.QRPoll{}, err
		}
		return online.QRPoll{Status: online.QRConfirmed, Account: &acc}, nil
	}
	return online.QRPoll{}, classify(r.Code, r.Message)
}

func (p *Plugin) LoginCookie(ctx context.Context, cookie string) (online.Account, error) {
	m := online.ParseCookieString(cookie)
	if m["MUSIC_U"] == "" {
		return online.Account{}, fmt.Errorf("%w: cookie 中缺少 MUSIC_U", online.ErrNeedsLogin)
	}
	keep := map[string]string{}
	for _, k := range []string{"MUSIC_U", "__csrf", "NMTID", "_ntes_nuid", "deviceId", "MUSIC_A_T", "MUSIC_R_T"} {
		if v := m[k]; v != "" {
			keep[k] = v
		}
	}
	old := p.c.Cookies()
	p.c.ReplaceCookies(keep)
	acc, err := p.Account(ctx)
	if err != nil {
		p.c.ReplaceCookies(old)
		return acc, err
	}
	return acc, nil
}

var digits = regexp.MustCompile(`^\+?\d{6,15}$`)

func (p *Plugin) LoginPassword(ctx context.Context, user, pass string) (online.Account, error) {
	user = strings.TrimSpace(user)
	if user == "" || pass == "" {
		return online.Account{}, fmt.Errorf("%w: 需要账号和密码", online.ErrNeedsLogin)
	}
	var r struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Msg     string `json:"msg"`
	}
	var body []byte
	var err error
	if digits.MatchString(user) {
		cc, phone := "86", strings.TrimPrefix(user, "+")
		if strings.HasPrefix(user, "+") && len(phone) > 11 {
			cc, phone = phone[:len(phone)-11], phone[len(phone)-11:]
		}
		body, err = p.weapiRaw(ctx, "/api/w/login/cellphone", map[string]any{"phone": phone, "countrycode": cc,
			"password": md5hex(pass), "remember": "true", "type": "1", "https": "true"})
	} else {
		body, err = p.eapiRaw(ctx, "/api/w/login", map[string]any{"username": user, "password": md5hex(pass),
			"rememberLogin": "true", "type": "0", "https": "true"})
	}
	if err != nil {
		return online.Account{}, err
	}
	if err := jsonUnmarshal(body, &r); err != nil {
		return online.Account{}, err
	}
	switch r.Code {
	case 200:
	case 502, 501, 400:
		return online.Account{}, fmt.Errorf("%w: 账号或密码错误", online.ErrNeedsLogin)
	case 8821, 8810, -462:
		return online.Account{}, fmt.Errorf("%w: 平台要求安全验证，请改用扫码登录", online.ErrRiskControl)
	default:
		msg := r.Message
		if msg == "" {
			msg = r.Msg
		}
		return online.Account{}, classify(r.Code, msg)
	}
	return p.Account(ctx)
}

// splitPhone accepts "13312345678", "+8613312345678" or "+86 133…".
func splitPhone(in string) (cc, phone string, err error) {
	in = strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(in))
	if !digits.MatchString(in) {
		return "", "", fmt.Errorf("%w: 手机号格式不正确", online.ErrNeedsLogin)
	}
	cc, phone = "86", strings.TrimPrefix(in, "+")
	if strings.HasPrefix(in, "+") && len(phone) > 11 {
		cc, phone = phone[:len(phone)-11], phone[len(phone)-11:]
	}
	return cc, phone, nil
}

// SendSMS asks NetEase to text a login code (weapi /sms/captcha/sent, PC identity).
func (p *Plugin) SendSMS(ctx context.Context, phone string) error {
	cc, ph, err := splitPhone(phone)
	if err != nil {
		return err
	}
	body, err := p.weapiRaw(ctx, "/api/sms/captcha/sent", map[string]any{"ctcode": cc, "secrete": "music_middleuser_pclogin", "cellphone": ph})
	if err != nil {
		return err
	}
	var r struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Msg     string `json:"msg"`
	}
	if err := jsonUnmarshal(body, &r); err != nil {
		return err
	}
	if r.Code == 200 {
		return nil
	}
	msg := firstNonEmpty(r.Message, r.Msg)
	switch r.Code {
	case 8821, 8810, -462:
		return fmt.Errorf("%w: 平台风控（%d）%s", online.ErrRiskControl, r.Code, msg)
	case 400, 503, 405:
		return fmt.Errorf("%w: 发送失败（%d）%s", online.ErrRiskControl, r.Code, msg)
	}
	return classify(r.Code, fmt.Sprintf("%d %s", r.Code, msg))
}

// LoginSMS logs in with phone + SMS code (weapi /w/login/cellphone).
func (p *Plugin) LoginSMS(ctx context.Context, phone, code string) (online.Account, error) {
	cc, ph, err := splitPhone(phone)
	if err != nil {
		return online.Account{}, err
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return online.Account{}, fmt.Errorf("%w: 需要验证码", online.ErrNeedsLogin)
	}
	body, err := p.weapiRaw(ctx, "/api/w/login/cellphone", map[string]any{"phone": ph, "countrycode": cc, "captcha": code,
		"remember": "true", "type": "1", "https": "true", "secureCaptcha": ""})
	if err != nil {
		return online.Account{}, err
	}
	var r struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Msg     string `json:"msg"`
	}
	if err := jsonUnmarshal(body, &r); err != nil {
		return online.Account{}, err
	}
	switch r.Code {
	case 200:
	case 503, 400, 501, 502:
		return online.Account{}, fmt.Errorf("%w: 验证码错误或已过期（%d）", online.ErrNeedsLogin, r.Code)
	case 8821, 8810, -462:
		return online.Account{}, fmt.Errorf("%w: 平台风控（%d）", online.ErrRiskControl, r.Code)
	default:
		return online.Account{}, classify(r.Code, fmt.Sprintf("%d %s", r.Code, firstNonEmpty(r.Message, r.Msg)))
	}
	if p.c.Cookie("MUSIC_U") == "" {
		return online.Account{}, fmt.Errorf("%w: login ok but no session cookie", online.ErrUpstream)
	}
	return p.Account(ctx)
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

func (p *Plugin) Account(ctx context.Context) (online.Account, error) {
	if p.c.Cookie("MUSIC_U") == "" {
		return online.Account{}, online.ErrNeedsLogin
	}
	var r struct {
		Account *struct {
			ID      int64 `json:"id"`
			VipType int   `json:"vipType"`
		} `json:"account"`
		Profile *struct {
			UserID    int64  `json:"userId"`
			Nickname  string `json:"nickname"`
			AvatarURL string `json:"avatarUrl"`
			VipType   int    `json:"vipType"`
		} `json:"profile"`
	}
	if err := p.weapi(ctx, "/api/w/nuser/account/get", nil, &r); err != nil {
		return online.Account{}, err
	}
	if r.Profile == nil {
		return online.Account{}, online.ErrNeedsLogin
	}
	vip := r.Profile.VipType
	if r.Account != nil && r.Account.VipType > vip {
		vip = r.Account.VipType
	}
	acc := online.Account{UserID: strconv.FormatInt(r.Profile.UserID, 10), Nickname: r.Profile.Nickname, Avatar: r.Profile.AvatarURL, VIP: vip > 0}
	acc.Tier = "免费"
	if vip > 0 {
		acc.Tier = "黑胶 VIP"
	}
	return acc, nil
}

func (p *Plugin) Refresh(ctx context.Context) error {
	if p.c.Cookie("MUSIC_U") == "" {
		return online.ErrNeedsLogin
	}
	return p.eapi(ctx, "/api/login/token/refresh", nil, nil)
}

func (p *Plugin) uid(ctx context.Context) (string, error) {
	acc, err := p.Account(ctx)
	if err != nil {
		return "", err
	}
	return acc.UserID, nil
}

// ── catalog ────────────────────────────────────────────────────────────────

func (p *Plugin) Search(ctx context.Context, q string, limit int) (online.SearchResult, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	out := online.SearchResult{Tracks: []online.Track{}, Albums: []online.Album{}, Artists: []online.Artist{}, Playlists: []online.Playlist{}}
	var songs struct {
		Result struct {
			Songs []neSong `json:"songs"`
		} `json:"result"`
	}
	if err := p.eapi(ctx, "/api/cloudsearch/pc", map[string]any{"s": q, "type": 1, "limit": limit, "offset": 0, "total": true}, &songs); err != nil {
		return out, err
	}
	out.Tracks = mapSongs(songs.Result.Songs, nil)
	small := min(limit, 12)
	var albums struct {
		Result struct {
			Albums []neAlbum `json:"albums"`
		} `json:"result"`
	}
	if err := p.eapi(ctx, "/api/cloudsearch/pc", map[string]any{"s": q, "type": 10, "limit": small, "offset": 0, "total": true}, &albums); err == nil {
		for _, a := range albums.Result.Albums {
			out.Albums = append(out.Albums, mapAlbum(a))
		}
	}
	var artists struct {
		Result struct {
			Artists []neArtist `json:"artists"`
		} `json:"result"`
	}
	if err := p.eapi(ctx, "/api/cloudsearch/pc", map[string]any{"s": q, "type": 100, "limit": small, "offset": 0, "total": true}, &artists); err == nil {
		for _, a := range artists.Result.Artists {
			out.Artists = append(out.Artists, mapArtist(a))
		}
	}
	var pls struct {
		Result struct {
			Playlists []nePlaylist `json:"playlists"`
		} `json:"result"`
	}
	if err := p.eapi(ctx, "/api/cloudsearch/pc", map[string]any{"s": q, "type": 1000, "limit": small, "offset": 0, "total": true}, &pls); err == nil {
		for _, pl := range pls.Result.Playlists {
			out.Playlists = append(out.Playlists, mapPlaylist(pl, "playlist"))
		}
	}
	return out, nil
}

func parseIDs(ids []string) ([]int64, error) {
	out := make([]int64, 0, len(ids))
	for _, s := range ids {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("%w: bad netease id %q", online.ErrUnavailable, s)
		}
		out = append(out, n)
	}
	return out, nil
}

func (p *Plugin) HotSearch(ctx context.Context) ([]online.HotKeyword, error) {
	var r struct {
		Data []struct {
			SearchWord string `json:"searchWord"`
			Score      int64  `json:"score"`
			URL        string `json:"url"`
		} `json:"data"`
	}
	if err := p.weapi(ctx, "/api/search/hot/detail", map[string]any{}, &r); err != nil {
		return nil, err
	}
	out := make([]online.HotKeyword, 0, len(r.Data))
	for _, d := range r.Data {
		kw := strings.TrimSpace(d.SearchWord)
		if kw == "" {
			continue
		}
		out = append(out, online.HotKeyword{Keyword: kw, Score: d.Score, URL: d.URL})
	}
	return out, nil
}

func (p *Plugin) Tracks(ctx context.Context, ids []string) ([]online.Track, error) {
	nums, err := parseIDs(ids)
	if err != nil {
		return nil, err
	}
	out := []online.Track{}
	for i := 0; i < len(nums); i += 500 {
		chunk := nums[i:min(len(nums), i+500)]
		parts := make([]string, len(chunk))
		for j, n := range chunk {
			parts[j] = fmt.Sprintf(`{"id":%d}`, n)
		}
		var r struct {
			Songs      []neSong      `json:"songs"`
			Privileges []nePrivilege `json:"privileges"`
		}
		if err := p.weapi(ctx, "/api/v3/song/detail", map[string]any{"c": "[" + strings.Join(parts, ",") + "]"}, &r); err != nil {
			return nil, err
		}
		out = append(out, mapSongs(r.Songs, r.Privileges)...)
	}
	return out, nil
}

func (p *Plugin) Album(ctx context.Context, id string) (online.Album, error) {
	if _, err := parseIDs([]string{id}); err != nil {
		return online.Album{}, err
	}
	var r struct {
		Album neAlbum  `json:"album"`
		Songs []neSong `json:"songs"`
	}
	if err := p.weapi(ctx, "/api/v1/album/"+id, nil, &r); err != nil {
		return online.Album{}, err
	}
	a := mapAlbum(r.Album)
	a.Tracks = mapSongs(r.Songs, nil)
	if a.TrackCount == 0 {
		a.TrackCount = len(a.Tracks)
	}
	for i := range a.Tracks {
		if a.Tracks[i].CoverURL == "" {
			a.Tracks[i].CoverURL = a.CoverURL
		}
	}
	return a, nil
}

func (p *Plugin) Artist(ctx context.Context, id string) (online.Artist, error) {
	if _, err := parseIDs([]string{id}); err != nil {
		return online.Artist{}, err
	}
	var r struct {
		Artist   neArtist `json:"artist"`
		HotSongs []neSong `json:"hotSongs"`
	}
	if err := p.weapi(ctx, "/api/v1/artist/"+id, nil, &r); err != nil {
		return online.Artist{}, err
	}
	a := mapArtist(r.Artist)
	a.TopTracks = mapSongs(r.HotSongs, nil)
	return a, nil
}

const maxPlaylistTracks = 1000

func (p *Plugin) Playlist(ctx context.Context, id string) (online.Playlist, error) {
	if _, err := parseIDs([]string{id}); err != nil {
		return online.Playlist{}, err
	}
	var r struct {
		Playlist   nePlaylist    `json:"playlist"`
		Privileges []nePrivilege `json:"privileges"`
	}
	if err := p.eapi(ctx, "/api/v6/playlist/detail", map[string]any{"id": id, "n": 100000, "s": 8}, &r); err != nil {
		return online.Playlist{}, err
	}
	pl := mapPlaylist(r.Playlist, "playlist")
	pl.Tracks = mapSongs(r.Playlist.Tracks, r.Privileges)
	if len(r.Playlist.TrackIDs) > len(pl.Tracks) {
		byID := map[string]online.Track{}
		for _, t := range pl.Tracks {
			byID[t.ID] = t
		}
		var rest []string
		for i, t := range r.Playlist.TrackIDs {
			if i >= maxPlaylistTracks {
				break
			}
			if _, ok := byID[fid(t.ID)]; !ok {
				rest = append(rest, strconv.FormatInt(t.ID, 10))
			}
		}
		if len(rest) > 0 {
			more, err := p.Tracks(ctx, rest)
			if err != nil {
				return pl, err
			}
			for _, t := range more {
				if _, ok := byID[t.ID]; !ok {
					byID[t.ID] = t
				}
			}
		}
		// rebuild in playlist order, deduplicated
		ordered := make([]online.Track, 0, len(r.Playlist.TrackIDs))
		seen := map[string]bool{}
		for i, t := range r.Playlist.TrackIDs {
			if i >= maxPlaylistTracks {
				break
			}
			id := fid(t.ID)
			if tr, ok := byID[id]; ok && !seen[id] {
				seen[id] = true
				ordered = append(ordered, tr)
			}
		}
		pl.Tracks = ordered
	}
	return pl, nil
}

func (p *Plugin) Lyrics(ctx context.Context, id string) (online.Lyrics, error) {
	var r struct {
		Lrc *struct {
			Lyric string `json:"lyric"`
		} `json:"lrc"`
		Tlyric *struct {
			Lyric string `json:"lyric"`
		} `json:"tlyric"`
		Romalrc *struct {
			Lyric string `json:"lyric"`
		} `json:"romalrc"`
		Yrc *struct {
			Lyric string `json:"lyric"`
		} `json:"yrc"`
		Nolyric bool `json:"nolyric"`
	}
	if err := p.eapi(ctx, "/api/song/lyric/v1", map[string]any{"id": id, "cp": false, "tv": 0, "lv": 0, "rv": 0, "kv": 0, "yv": 0, "ytv": 0, "yrv": 0}, &r); err != nil {
		return online.Lyrics{}, err
	}
	if r.Nolyric || r.Lrc == nil || strings.TrimSpace(r.Lrc.Lyric) == "" {
		return online.Lyrics{}, fmt.Errorf("%w: no lyrics", online.ErrUnavailable)
	}
	l := online.Lyrics{LRC: r.Lrc.Lyric, Synced: strings.Contains(r.Lrc.Lyric, "[0") || strings.Contains(r.Lrc.Lyric, "[1")}
	if r.Tlyric != nil {
		l.Translation = r.Tlyric.Lyric
	}
	if r.Romalrc != nil {
		l.Roma = r.Romalrc.Lyric
	}
	if r.Yrc != nil && r.Yrc.Lyric != "" {
		l.Words = lyrics.ParseYRC(r.Yrc.Lyric)
	}
	return l, nil
}

// ── discovery ──────────────────────────────────────────────────────────────

func (p *Plugin) Charts(ctx context.Context) ([]online.Playlist, error) {
	var r struct {
		List []nePlaylist `json:"list"`
	}
	if err := p.eapi(ctx, "/api/toplist", nil, &r); err != nil {
		return nil, err
	}
	out := make([]online.Playlist, 0, len(r.List))
	for _, pl := range r.List {
		out = append(out, mapPlaylist(pl, "chart"))
	}
	return out, nil
}

func (p *Plugin) RecommendTracks(ctx context.Context) ([]online.Track, error) {
	var r struct {
		Data struct {
			DailySongs []neSong      `json:"dailySongs"`
			Privileges []nePrivilege `json:"privileges"`
		} `json:"data"`
	}
	if err := p.weapi(ctx, "/api/v3/discovery/recommend/songs", map[string]any{}, &r); err != nil {
		return nil, err
	}
	return mapSongs(r.Data.DailySongs, r.Data.Privileges), nil
}

func (p *Plugin) RecommendPlaylists(ctx context.Context) ([]online.Playlist, error) {
	if p.c.Cookie("MUSIC_U") != "" {
		var r struct {
			Recommend []nePlaylist `json:"recommend"`
		}
		if err := p.weapi(ctx, "/api/v1/discovery/recommend/resource", nil, &r); err == nil {
			out := make([]online.Playlist, 0, len(r.Recommend))
			for _, pl := range r.Recommend {
				out = append(out, mapPlaylist(pl, "playlist"))
			}
			return out, nil
		}
	}
	var r struct {
		Result []nePlaylist `json:"result"`
	}
	if err := p.weapi(ctx, "/api/personalized/playlist", map[string]any{"limit": 30, "total": true, "n": 1000}, &r); err != nil {
		return nil, err
	}
	out := make([]online.Playlist, 0, len(r.Result))
	for _, pl := range r.Result {
		out = append(out, mapPlaylist(pl, "playlist"))
	}
	return out, nil
}

func (p *Plugin) DiscoverPlaylists(ctx context.Context, cat string, offset, limit int) ([]online.Playlist, error) {
	if cat == "" {
		cat = "全部"
	}
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	var r struct {
		Playlists []nePlaylist `json:"playlists"`
	}
	if err := p.weapi(ctx, "/api/playlist/list", map[string]any{"cat": cat, "order": "hot", "limit": limit, "offset": offset, "total": true}, &r); err != nil {
		return nil, err
	}
	out := make([]online.Playlist, 0, len(r.Playlists))
	for _, pl := range r.Playlists {
		out = append(out, mapPlaylist(pl, "playlist"))
	}
	return out, nil
}

func (p *Plugin) UserPlaylists(ctx context.Context) ([]online.Playlist, error) {
	uid, err := p.uid(ctx)
	if err != nil {
		return nil, err
	}
	var r struct {
		Playlist []nePlaylist `json:"playlist"`
	}
	if err := p.weapi(ctx, "/api/user/playlist", map[string]any{"uid": uid, "limit": 1000, "offset": 0, "includeVideo": true}, &r); err != nil {
		return nil, err
	}
	out := make([]online.Playlist, 0, len(r.Playlist))
	for _, pl := range r.Playlist {
		m := mapPlaylist(pl, "playlist")
		m.Mine = pl.Creator != nil && strconv.FormatInt(pl.Creator.UserID, 10) == uid
		out = append(out, m)
	}
	return out, nil
}

// ── stream ─────────────────────────────────────────────────────────────────

type neURL struct {
	ID            int64  `json:"id"`
	URL           string `json:"url"`
	Br            int    `json:"br"`
	Size          int64  `json:"size"`
	Code          int    `json:"code"`
	Expi          int    `json:"expi"`
	Type          string `json:"type"`
	Level         string `json:"level"`
	Fee           int    `json:"fee"`
	Time          int64  `json:"time"`
	FreeTrialInfo *struct {
		Start float64 `json:"start"`
		End   float64 `json:"end"`
	} `json:"freeTrialInfo"`
}

// Resolve asks the official player endpoint for this account's URL. When the
// platform answers with a trial segment (freeTrialInfo) the stream is marked
// preview with its window; no other source is consulted here.
func (p *Plugin) Resolve(ctx context.Context, id string, q online.Quality) (online.Stream, error) {
	if _, err := parseIDs([]string{id}); err != nil {
		return online.Stream{}, err
	}
	level := qualityLevel[q]
	if level == "" {
		level = "exhigh"
	}
	var r struct {
		Data []neURL `json:"data"`
	}
	if err := p.eapi(ctx, "/api/song/enhance/player/url/v1", map[string]any{"ids": "[" + id + "]", "level": level, "encodeType": "flac"}, &r); err != nil {
		return online.Stream{}, err
	}
	if len(r.Data) == 0 {
		return online.Stream{Entitlement: online.EntUnavailable, Reason: "平台未返回播放地址"}, nil
	}
	return streamFrom(r.Data[0]), nil
}

func streamFrom(d neURL) online.Stream {
	if d.URL == "" || d.Code != 200 {
		reason := "当前账号无法播放该歌曲"
		if d.Fee == 1 {
			reason = "会员专享歌曲，当前账号无试听权限"
		} else if d.Fee == 4 {
			reason = "付费专辑歌曲，需要购买"
		}
		return online.Stream{Entitlement: online.EntUnavailable, Reason: reason}
	}
	s := online.Stream{URL: d.URL, Entitlement: online.EntFull, Quality: levelQuality[d.Level], Format: strings.ToLower(d.Type),
		BitrateKbps: d.Br / 1000, SizeBytes: d.Size}
	if d.Expi > 0 {
		s.ExpiresAt = time.Now().Add(time.Duration(d.Expi) * time.Second)
	}
	if d.FreeTrialInfo != nil {
		s.Entitlement = online.EntPreview
		s.PreviewStartMs = int64(d.FreeTrialInfo.Start * 1000)
		s.PreviewEndMs = int64(d.FreeTrialInfo.End * 1000)
		s.Reason = "非会员仅可试听片段"
	}
	if s.Format == "" {
		s.Format = "mp3"
	}
	return s
}

// FM is the account's 私人FM (a few tracks per call, as the platform serves them).
func (p *Plugin) FM(ctx context.Context) ([]online.Track, error) {
	if p.c.Cookie("MUSIC_U") == "" {
		return nil, online.ErrNeedsLogin
	}
	var r struct {
		Data []neSong `json:"data"`
	}
	if err := p.weapi(ctx, "/api/v1/radio/get", map[string]any{}, &r); err != nil {
		return nil, err
	}
	return mapSongs(r.Data, nil), nil
}

// FMFeedback sends 喜欢 (like) or 不喜欢 (trash) for an FM track.
func (p *Plugin) FMFeedback(ctx context.Context, nativeID, action string) error {
	if p.c.Cookie("MUSIC_U") == "" {
		return online.ErrNeedsLogin
	}
	id, err := strconv.ParseInt(nativeID, 10, 64)
	if err != nil {
		return online.ErrBadInput
	}
	var r struct {
		Code int `json:"code"`
	}
	switch action {
	case "like":
		return p.weapi(ctx, "/api/radio/like", map[string]any{"alg": "itembased", "trackId": id, "like": true, "time": "3"}, &r)
	case "trash":
		return p.weapi(ctx, "/api/radio/trash/add", map[string]any{"alg": "RT", "songId": id, "time": "3"}, &r)
	}
	return online.ErrBadInput
}

// qrPC selects the desktop identity for QR login (YIMENG_NETEASE_QR_PC=1).
var qrPC = os.Getenv("YIMENG_NETEASE_QR_PC") == "1"
