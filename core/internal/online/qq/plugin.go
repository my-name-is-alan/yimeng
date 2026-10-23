// Package qq is the QQ音乐 plugin: a native Go client for musicu.fcg with our
// own ptlogin QR login. It plays only what the account is entitled to: the
// full file when the vkey service grants it, otherwise QQ Music's official
// trial clip (RS02) for paid songs, otherwise nothing. No decryption of
// qmc/mflac, no unblocking, no cross-platform URL borrowing. Protocol
// knowledge from qq-music-api (MIT, Rain120); the implementation is our own.
package qq

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/online"
)

type Plugin struct {
	api
	cfg online.Config
}

func New(o Options) online.Factory {
	return func(env online.Env) (online.Plugin, error) {
		return &Plugin{api: api{c: env.HTTP, opts: o, now: time.Now}, cfg: env.Config}, nil
	}
}

func (p *Plugin) Manifest() online.Manifest {
	return online.Manifest{
		ID: origin, Name: "QQ音乐", Version: "1.0.0", Homepage: "https://y.qq.com",
		Description: "搜索、歌词、排行榜、推荐、歌单广场、我的歌单与播放（按账号权益）。",
		Capabilities: []online.Capability{online.CapSearch, online.CapLyrics, online.CapCharts, online.CapRecommend,
			online.CapDiscover, online.CapUserPlaylists, online.CapStream, online.CapHotSearch, online.CapArtistAlbums},
		LoginMethods: []online.LoginMethod{online.LoginQR, online.LoginCookie},
		QRChannels:   []online.QRChannel{{ID: "qq", Label: "QQ 扫码", App: "手机 QQ", Hint: "请使用 QQ 扫码（手机 QQ App，不是 QQ音乐 或 微信）"}},
		Qualities:    []online.Quality{online.QStandard, online.QHigh, online.QLossless},
		DefaultRate:  40,
		Notice:       "仅播放当前账号有权收听的内容：付费/会员歌曲在非会员账号下只播放 QQ 音乐官方试听片段。不解密 qmc/mflac，不绕过任何限制。",
		Credits:      []string{"qq-music-api (MIT, Rain120) — protocol reference"},
	}
}

func (p *Plugin) vip() bool { return p.loggedIn() && p.c.Cookie(kVIP) == "1" }

// ── catalog ────────────────────────────────────────────────────────────────

func searchReq(q string, typ, n int) mreq {
	return mreq{"music.search.SearchCgiService", "DoSearchForQQMusicDesktop", map[string]any{
		"search_type": typ, "query": q, "page_num": 1, "num_per_page": n}}
}

func (p *Plugin) Search(ctx context.Context, q string, limit int) (online.SearchResult, error) {
	if limit <= 0 || limit > 50 {
		limit = 30
	}
	small := min(limit, 10)
	out, codes, err := p.muComm(ctx, true, map[string]mreq{"song": searchReq(q, 0, limit), "album": searchReq(q, 2, small),
		"pl": searchReq(q, 3, small), "singer": searchReq(q, 1, small)})
	res := online.SearchResult{}
	if err != nil {
		return res, err
	}
	if err := classify(codes["song"], "search"); err != nil {
		return res, err
	}
	var s struct {
		Body struct {
			Song struct {
				List []qSong `json:"list"`
			} `json:"song"`
			Album struct {
				List []struct {
					AlbumMID   string    `json:"albumMID"`
					AlbumName  string    `json:"albumName"`
					PublicTime string    `json:"publicTime"`
					SongCount  int       `json:"song_count"`
					SingerList []qSinger `json:"singer_list"`
				} `json:"list"`
			} `json:"album"`
			Songlist struct {
				List []struct {
					DissID   string `json:"dissid"`
					DissName string `json:"dissname"`
					ImgURL   string `json:"imgurl"`
					Intro    string `json:"introduction"`
					SongNum  int    `json:"song_count"`
					Listen   int64  `json:"listennum"`
					Creator  struct {
						Name string `json:"name"`
					} `json:"creator"`
				} `json:"list"`
			} `json:"songlist"`
			Singer struct {
				List []struct {
					SingerMID  string `json:"singerMID"`
					SingerName string `json:"singerName"`
					AlbumNum   int    `json:"albumNum"`
					SongNum    int    `json:"songNum"`
				} `json:"list"`
			} `json:"singer"`
		} `json:"body"`
	}
	// Every sub-response carries all list keys (empty except its own type),
	// so decode each into a scratch value and keep only its own list.
	for _, k := range []string{"song", "album", "pl", "singer"} {
		if codes[k] != 0 {
			continue
		}
		t := s
		json.Unmarshal(out[k], &t)
		switch k {
		case "song":
			s.Body.Song = t.Body.Song
		case "album":
			s.Body.Album = t.Body.Album
		case "pl":
			s.Body.Songlist = t.Body.Songlist
		case "singer":
			s.Body.Singer = t.Body.Singer
		}
	}
	res.Tracks = mapSongs(s.Body.Song.List, p.vip())
	for _, a := range s.Body.Album.List {
		al := online.Album{ID: fid("album-" + a.AlbumMID), Origin: origin, Title: a.AlbumName, CoverURL: albumCover(a.AlbumMID, 500), TrackCount: a.SongCount}
		al.Artists, al.ArtistName = singerRefs(a.SingerList)
		if len(a.PublicTime) >= 4 {
			al.Year, _ = strconv.Atoi(a.PublicTime[:4])
		}
		res.Albums = append(res.Albums, al)
	}
	for _, a := range s.Body.Singer.List {
		res.Artists = append(res.Artists, online.Artist{ID: fid("artist-" + a.SingerMID), Origin: origin, Name: a.SingerName,
			PictureURL: singerPic(a.SingerMID, 500), AlbumCount: a.AlbumNum, TrackCount: a.SongNum})
	}
	for _, d := range s.Body.Songlist.List {
		res.Playlists = append(res.Playlists, online.Playlist{ID: fid("pl-" + d.DissID), Origin: origin, Kind: "playlist", Name: d.DissName,
			CoverURL: https(d.ImgURL), Description: d.Intro, TrackCount: d.SongNum, Owner: d.Creator.Name, PlayCount: d.Listen})
	}
	return res, nil
}

func singerRefs(ss []qSinger) ([]online.Ref, string) {
	var refs []online.Ref
	var names []string
	for _, s := range ss {
		refs = append(refs, online.Ref{ID: fid("artist-" + s.Mid), Name: s.Name})
		names = append(names, s.Name)
	}
	return refs, strings.Join(names, " / ")
}

func (p *Plugin) trackInfo(ctx context.Context, mids []string) ([]qSong, error) {
	var out []qSong
	for start := 0; start < len(mids); start += 100 {
		end := min(start+100, len(mids))
		types := make([]int, end-start)
		var d struct {
			Tracks []qSong `json:"tracks"`
		}
		if err := p.one(ctx, "music.trackInfo.UniformRuleCtrl", "CgiGetTrackInfo",
			map[string]any{"mids": mids[start:end], "types": types, "ids": []int{}, "ctx": 0}, &d); err != nil {
			return nil, err
		}
		out = append(out, d.Tracks...)
	}
	return out, nil
}

func validMid(id string) error {
	if id == "" || strings.ContainsAny(id, "-:/ ") {
		return fmt.Errorf("%w: qq song id %q", online.ErrBadInput, id)
	}
	return nil
}

func (p *Plugin) HotSearch(ctx context.Context) ([]online.HotKeyword, error) {
	var d struct {
		VecHotkey []struct {
			Query   string          `json:"query"`
			Score   json.RawMessage `json:"score"` // upstream sends a decimal string
			JumpURL string          `json:"jump_url"`
		} `json:"vec_hotkey"`
	}
	// search_id is an opaque session token; a random hex is fine for anonymous hot keys.
	if err := p.one(ctx, "music.musicsearch.HotkeyService", "GetHotkeyForQQMusicMobile",
		map[string]any{"search_id": strconv.FormatInt(p.now().UnixNano(), 10)}, &d); err != nil {
		return nil, err
	}
	out := make([]online.HotKeyword, 0, len(d.VecHotkey))
	seen := map[string]bool{}
	for _, h := range d.VecHotkey {
		kw := strings.TrimSpace(h.Query)
		if kw == "" || seen[kw] {
			continue
		}
		seen[kw] = true
		out = append(out, online.HotKeyword{Keyword: kw, Score: parseFlexibleInt64(h.Score), URL: h.JumpURL})
	}
	return out, nil
}

func parseFlexibleInt64(raw json.RawMessage) int64 {
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	var n int64
	if json.Unmarshal(raw, &n) == nil {
		return n
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		n, _ = strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		return n
	}
	return 0
}

func (p *Plugin) Tracks(ctx context.Context, ids []string) ([]online.Track, error) {
	for _, id := range ids {
		if err := validMid(id); err != nil {
			return nil, err
		}
	}
	ss, err := p.trackInfo(ctx, ids)
	if err != nil {
		return nil, err
	}
	return mapSongs(ss, p.vip()), nil
}

type wrapped struct {
	SongInfo qSong `json:"songInfo"`
}

func unwrap(ws []wrapped) []qSong {
	out := make([]qSong, len(ws))
	for i, w := range ws {
		out[i] = w.SongInfo
	}
	return out
}

func (p *Plugin) Album(ctx context.Context, id string) (online.Album, error) {
	mid := strings.TrimPrefix(id, "album-")
	var d struct {
		BasicInfo struct {
			AlbumMid    string `json:"albumMid"`
			AlbumName   string `json:"albumName"`
			PublishDate string `json:"publishDate"`
			Desc        string `json:"desc"`
		} `json:"basicInfo"`
		Singer struct {
			SingerList []qSinger `json:"singerList"`
		} `json:"singer"`
	}
	if err := p.one(ctx, "music.musichallAlbum.AlbumInfoServer", "GetAlbumDetail", map[string]any{"albumMid": mid}, &d); err != nil {
		return online.Album{}, err
	}
	if d.BasicInfo.AlbumName == "" {
		return online.Album{}, fmt.Errorf("%w: album %s", online.ErrUnavailable, mid)
	}
	al := online.Album{ID: fid("album-" + mid), Origin: origin, Title: d.BasicInfo.AlbumName, CoverURL: albumCover(mid, 500), Description: d.BasicInfo.Desc}
	al.Artists, al.ArtistName = singerRefs(d.Singer.SingerList)
	if len(d.BasicInfo.PublishDate) >= 4 {
		al.Year, _ = strconv.Atoi(d.BasicInfo.PublishDate[:4])
	}
	var songs []qSong
	for begin := 0; begin < 1000; begin += 100 {
		var s struct {
			TotalNum int       `json:"totalNum"`
			SongList []wrapped `json:"songList"`
		}
		if err := p.one(ctx, "music.musichallAlbum.AlbumSongList", "GetAlbumSongList",
			map[string]any{"albumMid": mid, "begin": begin, "num": 100, "order": 2}, &s); err != nil {
			return al, err
		}
		songs = append(songs, unwrap(s.SongList)...)
		if len(s.SongList) < 100 || len(songs) >= s.TotalNum {
			break
		}
	}
	al.Tracks = mapSongs(songs, p.vip())
	al.TrackCount = len(al.Tracks)
	return al, nil
}

func (p *Plugin) Artist(ctx context.Context, id string) (online.Artist, error) {
	mid := strings.TrimPrefix(id, "artist-")
	var s struct {
		TotalNum int       `json:"totalNum"`
		SongList []wrapped `json:"songList"`
	}
	if err := p.one(ctx, "musichall.song_list_server", "GetSingerSongList",
		map[string]any{"singerMid": mid, "begin": 0, "num": 50, "order": 1}, &s); err != nil {
		return online.Artist{}, err
	}
	songs := unwrap(s.SongList)
	ar := online.Artist{ID: fid("artist-" + mid), Origin: origin, PictureURL: singerPic(mid, 500), TrackCount: s.TotalNum}
	for _, sg := range songs {
		for _, si := range sg.Singer {
			if si.Mid == mid {
				ar.Name = si.Name
			}
		}
	}
	if ar.Name == "" && len(songs) == 0 {
		return ar, fmt.Errorf("%w: artist %s", online.ErrUnavailable, mid)
	}
	ar.TopTracks = mapSongs(songs, p.vip())
	return ar, nil
}

func (p *Plugin) ArtistAlbums(ctx context.Context, id string, offset, limit int) (online.ArtistAlbumList, error) {
	mid := strings.TrimPrefix(id, "artist-")
	if err := validMid(mid); err != nil {
		return online.ArtistAlbumList{}, err
	}
	if limit <= 0 {
		limit = 30
	}
	if offset < 0 {
		offset = 0
	}
	var d struct {
		Total     int `json:"total"`
		AlbumList []struct {
			AlbumMid    string `json:"albumMid"`
			AlbumName   string `json:"albumName"`
			PublishDate string `json:"publishDate"`
			TotalNum    int    `json:"totalNum"`
			SingerName  string `json:"singerName"`
		} `json:"albumList"`
	}
	// order 0 = latest (by publish date) — natural for an artist albums tab.
	if err := p.one(ctx, "music.musichallAlbum.AlbumListServer", "GetAlbumList",
		map[string]any{"singerMid": mid, "order": 0, "num": limit, "begin": offset}, &d); err != nil {
		return online.ArtistAlbumList{}, err
	}
	out := online.ArtistAlbumList{Total: d.Total, Items: make([]online.Album, 0, len(d.AlbumList))}
	for _, a := range d.AlbumList {
		if a.AlbumMid == "" || a.AlbumName == "" {
			continue
		}
		al := online.Album{
			ID: fid("album-" + a.AlbumMid), Origin: origin, Title: a.AlbumName,
			CoverURL: albumCover(a.AlbumMid, 500), TrackCount: a.TotalNum, // upstream often sends 0 here
		}
		if a.SingerName != "" {
			al.Artists = []online.Ref{{ID: fid("artist-" + mid), Name: a.SingerName}}
			al.ArtistName = a.SingerName
		}
		if len(a.PublishDate) >= 4 {
			al.Year, _ = strconv.Atoi(a.PublishDate[:4])
		}
		out.Items = append(out.Items, al)
		if len(out.Items) >= limit {
			break
		}
	}
	return out, nil
}

func (p *Plugin) Playlist(ctx context.Context, id string) (online.Playlist, error) {
	switch {
	case strings.HasPrefix(id, "top-"):
		return p.toplist(ctx, strings.TrimPrefix(id, "top-"))
	case strings.HasPrefix(id, "pl-"):
		return p.diss(ctx, strings.TrimPrefix(id, "pl-"))
	}
	return online.Playlist{}, fmt.Errorf("%w: qq playlist id %q", online.ErrBadInput, id)
}

func (p *Plugin) diss(ctx context.Context, did string) (online.Playlist, error) {
	n, err := strconv.ParseInt(did, 10, 64)
	if err != nil {
		return online.Playlist{}, fmt.Errorf("%w: qq playlist id %q", online.ErrBadInput, did)
	}
	pl := online.Playlist{ID: fid("pl-" + did), Origin: origin, Kind: "playlist"}
	var songs []qSong
	for begin := 0; begin < 3000; begin += 500 {
		var d struct {
			Code    int `json:"code"`
			DirInfo struct {
				Title     string `json:"title"`
				PicURL    string `json:"picurl"`
				Desc      string `json:"desc"`
				HostNick  string `json:"host_nick"`
				HostUin   int64  `json:"host_uin"`
				DirID     int    `json:"dirid"`
				SongNum   int    `json:"songnum"`
				ListenNum int64  `json:"listennum"`
			} `json:"dirinfo"`
			SongList []qSong `json:"songlist"`
			Total    int     `json:"total_song_num"`
		}
		if err := p.one(ctx, "srf_diss_info.DissInfoServer", "CgiGetDiss", map[string]any{"disstid": n, "onlysong": 0,
			"song_begin": begin, "song_num": 500, "userinfo": 1, "tag": 1, "orderlist": 1}, &d); err != nil {
			if begin == 0 {
				return pl, err
			}
			break
		}
		if begin == 0 {
			if d.DirInfo.Title == "" && len(d.SongList) == 0 {
				return pl, fmt.Errorf("%w: playlist %s", online.ErrUnavailable, did)
			}
			pl.Name, pl.CoverURL, pl.Description, pl.Owner = d.DirInfo.Title, https(d.DirInfo.PicURL), d.DirInfo.Desc, d.DirInfo.HostNick
			pl.TrackCount, pl.PlayCount = d.DirInfo.SongNum, d.DirInfo.ListenNum
			pl.Mine = p.loggedIn() && strconv.FormatInt(d.DirInfo.HostUin, 10) == p.uin()
			if pl.Mine && d.DirInfo.DirID == 201 {
				pl.Kind = "liked"
			}
		}
		songs = append(songs, d.SongList...)
		if len(d.SongList) < 500 || len(songs) >= d.Total {
			break
		}
	}
	pl.Tracks = mapSongs(songs, p.vip())
	if pl.TrackCount < len(pl.Tracks) {
		pl.TrackCount = len(pl.Tracks)
	}
	return pl, nil
}

type qTop struct {
	TopID      int    `json:"topId"`
	Title      string `json:"title"`
	Intro      string `json:"intro"`
	Period     string `json:"period"`
	UpdateTime string `json:"updateTime"`
	ListenNum  int64  `json:"listenNum"`
	TotalNum   int    `json:"totalNum"`
	FrontPic   string `json:"frontPicUrl"`
	HeadPic    string `json:"headPicUrl"`
	MbFrontPic string `json:"mbFrontPicUrl"`
}

func mapTop(t qTop) online.Playlist {
	cover := t.FrontPic
	if cover == "" {
		cover = t.MbFrontPic
	}
	return online.Playlist{ID: fid("top-" + strconv.Itoa(t.TopID)), Origin: origin, Kind: "chart", Name: t.Title,
		CoverURL: https(cover), Description: strings.ReplaceAll(t.Intro, "<br>", "\n"), TrackCount: t.TotalNum,
		PlayCount: t.ListenNum, Subtitle: t.UpdateTime}
}

func (p *Plugin) toplist(ctx context.Context, tid string) (online.Playlist, error) {
	n, err := strconv.Atoi(tid)
	if err != nil {
		return online.Playlist{}, fmt.Errorf("%w: qq chart id %q", online.ErrBadInput, tid)
	}
	var d struct {
		Data         qTop    `json:"data"`
		SongInfoList []qSong `json:"songInfoList"`
	}
	if err := p.one(ctx, "musicToplist.ToplistInfoServer", "GetDetail", map[string]any{"topid": n, "offset": 0, "num": 300, "period": ""}, &d); err != nil {
		return online.Playlist{}, err
	}
	pl := mapTop(d.Data)
	pl.Tracks = mapSongs(d.SongInfoList, p.vip())
	if len(pl.Tracks) > 0 {
		pl.TrackCount = len(pl.Tracks)
	}
	return pl, nil
}

// Lyrics: GetPlayLyricInfo returns base64 LRC (crypt=0). Encrypted QRC is
// never requested or decoded.
func (p *Plugin) Lyrics(ctx context.Context, id string) (online.Lyrics, error) {
	if err := validMid(id); err != nil {
		return online.Lyrics{}, err
	}
	var d struct {
		Lyric string `json:"lyric"`
		Trans string `json:"trans"`
		Crypt int    `json:"crypt"`
	}
	if err := p.one(ctx, "music.musichallSong.PlayLyricInfo", "GetPlayLyricInfo", map[string]any{"songMID": id, "songID": 0}, &d); err != nil {
		return online.Lyrics{}, err
	}
	if d.Crypt != 0 || d.Lyric == "" {
		return online.Lyrics{}, fmt.Errorf("%w: no plain lyrics", online.ErrUnavailable)
	}
	dec := func(s string) string {
		b, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return ""
		}
		return strings.ReplaceAll(string(b), "\r\n", "\n")
	}
	l := online.Lyrics{LRC: dec(d.Lyric), Translation: dec(d.Trans)}
	l.Synced = strings.Contains(l.LRC, "[0")
	if l.LRC == "" {
		return l, fmt.Errorf("%w: empty lyrics", online.ErrUnavailable)
	}
	return l, nil
}

// ── discovery ──────────────────────────────────────────────────────────────

func (p *Plugin) Charts(ctx context.Context) ([]online.Playlist, error) {
	var d struct {
		Group []struct {
			Toplist []qTop `json:"toplist"`
		} `json:"group"`
	}
	if err := p.one(ctx, "musicToplist.ToplistInfoServer", "GetAll", map[string]any{}, &d); err != nil {
		return nil, err
	}
	var out []online.Playlist
	for _, g := range d.Group {
		for _, t := range g.Toplist {
			out = append(out, mapTop(t))
		}
	}
	return out, nil
}

// RecommendTracks: "猜你喜欢" radio for logged-in users, otherwise new songs.
func (p *Plugin) RecommendTracks(ctx context.Context) ([]online.Track, error) {
	if p.loggedIn() {
		// the personal radio hands out small batches (~5): collect up to 30
		var all []qSong
		seen := map[string]bool{}
		for i := 0; i < 8 && len(all) < 30; i++ {
			var d struct {
				Tracks []qSong `json:"tracks"`
			}
			err := p.one(ctx, "music.radioProxy.MbTrackRadioSvr", "get_radio_track", map[string]any{"id": 99, "num": 30, "from": 0,
				"scene": 0, "song_ids": []int{}, "ext": map[string]any{"bluetooth": ""}, "should_count_down": 1}, &d)
			if err != nil || len(d.Tracks) == 0 {
				break
			}
			added := 0
			for _, t := range d.Tracks {
				if k := t.Mid; k != "" && !seen[k] {
					seen[k] = true
					all = append(all, t)
					added++
				}
			}
			_ = added // duplicate batches happen; keep asking until 8 calls
		}
		if len(all) > 0 {
			return mapSongs(all, p.vip()), nil
		}
	}
	var d struct {
		SongList []qSong `json:"songlist"`
	}
	if err := p.one(ctx, "newsong.NewSongServer", "get_new_song_info", map[string]any{"type": 5}, &d); err != nil {
		return nil, err
	}
	if len(d.SongList) > 30 {
		d.SongList = d.SongList[:30]
	}
	return mapSongs(d.SongList, p.vip()), nil
}

func (p *Plugin) RecommendPlaylists(ctx context.Context) ([]online.Playlist, error) {
	var d struct {
		VHot []struct {
			ContentID int64  `json:"content_id"`
			Cover     string `json:"cover"`
			Title     string `json:"title"`
			ListenNum int64  `json:"listen_num"`
			Username  string `json:"username"`
		} `json:"v_hot"`
	}
	if err := p.one(ctx, "playlist.HotRecommendServer", "get_hot_recommend", map[string]any{"async": 1, "cmd": 2}, &d); err != nil {
		return nil, err
	}
	var out []online.Playlist
	for _, h := range d.VHot {
		if h.ContentID == 0 {
			continue
		}
		out = append(out, online.Playlist{ID: fid("pl-" + strconv.FormatInt(h.ContentID, 10)), Origin: origin, Kind: "playlist",
			Name: h.Title, CoverURL: https(h.Cover), PlayCount: h.ListenNum, Owner: h.Username})
	}
	return out, nil
}

func (p *Plugin) DiscoverPlaylists(ctx context.Context, cat string, offset, limit int) ([]online.Playlist, error) {
	if limit <= 0 || limit > 50 {
		limit = 30
	}
	tag, err := strconv.Atoi(cat)
	if err != nil || tag == 0 {
		tag = 10000000 // 全部
	}
	var d struct {
		VPlaylist []struct {
			TID         int64  `json:"tid"`
			Title       string `json:"title"`
			Desc        string `json:"desc"`
			CoverURL    string `json:"cover_url_medium"`
			AccessNum   int64  `json:"access_num"`
			CreatorInfo struct {
				Nick string `json:"nick"`
			} `json:"creator_info"`
		} `json:"v_playlist"`
	}
	if err := p.one(ctx, "playlist.PlayListPlazaServer", "get_playlist_by_tag", map[string]any{"id": tag, "sin": offset, "size": limit,
		"order": 5, "cur_page": offset/limit + 1}, &d); err != nil {
		return nil, err
	}
	var out []online.Playlist
	for _, v := range d.VPlaylist {
		out = append(out, online.Playlist{ID: fid("pl-" + strconv.FormatInt(v.TID, 10)), Origin: origin, Kind: "playlist", Name: v.Title,
			Description: v.Desc, CoverURL: https(v.CoverURL), PlayCount: v.AccessNum, Owner: v.CreatorInfo.Nick})
	}
	return out, nil
}

func (p *Plugin) UserPlaylists(ctx context.Context) ([]online.Playlist, error) {
	if !p.loggedIn() {
		return nil, online.ErrNeedsLogin
	}
	var d struct {
		VPlaylist []struct {
			TID     int64  `json:"tid"`
			DirID   int    `json:"dirId"`
			DirName string `json:"dirName"`
			SongNum int    `json:"songNum"`
			PicURL  string `json:"picUrl"`
		} `json:"v_playlist"`
	}
	if err := p.one(ctx, "music.musicasset.PlaylistBaseRead", "GetPlaylistByUin", map[string]any{"uin": p.uin()}, &d); err != nil {
		return nil, err
	}
	var out []online.Playlist
	for _, v := range d.VPlaylist {
		if v.TID == 0 {
			continue
		}
		pl := online.Playlist{ID: fid("pl-" + strconv.FormatInt(v.TID, 10)), Origin: origin, Kind: "playlist", Name: v.DirName,
			TrackCount: v.SongNum, CoverURL: https(v.PicURL), Mine: true}
		if v.DirID == 201 {
			pl.Kind, pl.Name = "liked", "我喜欢"
		}
		out = append(out, pl)
	}
	return out, nil
}

// ── stream ─────────────────────────────────────────────────────────────────

type qfile struct {
	prefix, ext string
	q           online.Quality
	kbps        int
	size        func(qSong) int64
}

// Highest first. C400 (96k AAC) is the last full-track fallback.
var ladder = []qfile{
	{"F000", "flac", online.QLossless, 900, func(s qSong) int64 { return s.File.SizeFlac }},
	{"M800", "mp3", online.QHigh, 320, func(s qSong) int64 { return s.File.Size320 }},
	{"M500", "mp3", online.QStandard, 128, func(s qSong) int64 { return s.File.Size128 }},
	{"C400", "m4a", online.QStandard, 96, func(s qSong) int64 { return s.File.Size96AAC }},
}

type vkeyResp struct {
	Sip        []string `json:"sip"`
	MidURLInfo []struct {
		Filename string `json:"filename"`
		Purl     string `json:"purl"`
		Result   int    `json:"result"`
	} `json:"midurlinfo"`
}

func (p *Plugin) vkey(ctx context.Context, mid string, files []string) (vkeyResp, error) {
	mids := make([]string, len(files))
	types := make([]int, len(files))
	for i := range files {
		mids[i] = mid
	}
	var v vkeyResp
	err := p.one(ctx, "vkey.GetVkeyServer", "CgiGetVkey", map[string]any{"filename": files, "guid": p.guid(), "songmid": mids,
		"songtype": types, "uin": p.uin(), "loginflag": 1, "platform": "20"}, &v)
	return v, err
}

// cdnHosts are QQ Music stream hosts tried after the vkey's own sips.
var cdnHosts = []string{"https://isure.stream.qqmusic.qq.com/", "http://ws.stream.qqmusic.qq.com/", "http://aqqmusic.tc.qq.com/", "https://dl.stream.qqmusic.qq.com/"}

// altURLs lists purl on every other known host (the first sip may answer an
// HTML "Forbidden" page for some regions/accounts).
func altURLs(sips []string, purl string) []string {
	first := pickSip(sips)
	seen := map[string]bool{first: true}
	var out []string
	for _, h := range append(append([]string{}, sips...), cdnHosts...) {
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		out = append(out, h+purl)
	}
	return out
}

func pickSip(sips []string) string {
	for _, s := range sips {
		if !strings.HasPrefix(s, "http://ws") {
			return s
		}
	}
	if len(sips) > 0 {
		return sips[0]
	}
	return "https://isure.stream.qqmusic.qq.com/"
}

func (p *Plugin) Resolve(ctx context.Context, id string, q online.Quality) (online.Stream, error) {
	if err := validMid(id); err != nil {
		return online.Stream{}, err
	}
	ss, err := p.trackInfo(ctx, []string{id})
	if err != nil {
		return online.Stream{}, err
	}
	if len(ss) == 0 || ss[0].Mid == "" {
		return online.Stream{Entitlement: online.EntUnavailable, Reason: "QQ 音乐未找到该歌曲"}, nil
	}
	s := ss[0]
	media := s.File.MediaMid
	if media == "" {
		media = s.Mid
	}
	want := q.Rank()
	if want == 0 {
		want = online.QHigh.Rank()
	}
	var cands []qfile
	var files []string
	for _, f := range ladder {
		if f.q.Rank() <= want && f.size(s) > 0 {
			cands = append(cands, f)
			files = append(files, f.prefix+s.Mid+media+"."+f.ext)
		}
	}
	dur := s.Interval * 1000
	if len(files) > 0 {
		v, err := p.vkey(ctx, s.Mid, files)
		if err != nil {
			return online.Stream{}, err
		}
		purls := map[string]string{}
		for _, m := range v.MidURLInfo {
			purls[m.Filename] = m.Purl
		}
		for i, f := range cands {
			if u := purls[files[i]]; u != "" && !strings.HasPrefix(u, "RS02") {
				return online.Stream{URL: pickSip(v.Sip) + u, Alternates: altURLs(v.Sip, u), Header: map[string]string{"Referer": "https://y.qq.com/", "User-Agent": uaWeb},
					Entitlement: online.EntFull, Quality: f.q, Format: f.ext, BitrateKbps: f.kbps, SizeBytes: f.size(s),
					DurationMs: dur, ExpiresAt: p.now().Add(20 * time.Minute)}, nil
			}
		}
	}
	if tf := s.trialFile(); tf != "" {
		v, err := p.vkey(ctx, s.Mid, []string{tf})
		if err != nil {
			return online.Stream{}, err
		}
		if len(v.MidURLInfo) > 0 && v.MidURLInfo[0].Purl != "" {
			end := s.File.TryEnd
			if end <= s.File.TryBegin {
				end = s.File.TryBegin + 30000
			}
			return online.Stream{URL: pickSip(v.Sip) + v.MidURLInfo[0].Purl, Alternates: altURLs(v.Sip, v.MidURLInfo[0].Purl), Header: map[string]string{"Referer": "https://y.qq.com/", "User-Agent": uaWeb},
				Entitlement: online.EntPreview, Quality: online.QStandard, Format: "mp3", BitrateKbps: 128, SizeBytes: s.File.SizeTry,
				DurationMs: dur, PreviewStartMs: s.File.TryBegin, PreviewEndMs: end,
				Reason: "付费/会员歌曲：当前账号仅可播放 QQ 音乐官方试听片段", ExpiresAt: p.now().Add(20 * time.Minute)}, nil
		}
	}
	return online.Stream{Entitlement: online.EntUnavailable, Reason: "QQ 音乐未向当前账号提供该歌曲（版权或付费限制）"}, nil
}
