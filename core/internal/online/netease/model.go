package netease

import (
	"strconv"
	"strings"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/online"
)

const origin = "netease"

func fid(id int64) string { return origin + ":" + strconv.FormatInt(id, 10) }

type neArtist struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	PicURL    string `json:"picUrl"`
	Img1v1URL string `json:"img1v1Url"`
	BriefDesc string `json:"briefDesc"`
	AlbumSize int    `json:"albumSize"`
	MusicSize int    `json:"musicSize"`
}

type neAlbum struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	PicURL      string     `json:"picUrl"`
	Artist      *neArtist  `json:"artist"`
	Artists     []neArtist `json:"artists"`
	PublishTime int64      `json:"publishTime"`
	Size        int        `json:"size"`
	Description string     `json:"description"`
}

type neTrialPriv struct {
	ResConsumable  bool `json:"resConsumable"`
	UserConsumable bool `json:"userConsumable"`
}

type nePrivilege struct {
	ID                 int64       `json:"id"`
	Fee                int         `json:"fee"`
	St                 int         `json:"st"`
	Pl                 int         `json:"pl"`
	Maxbr              int         `json:"maxbr"`
	PlLevel            string      `json:"plLevel"`
	MaxBrLevel         string      `json:"maxBrLevel"`
	FreeTrialPrivilege neTrialPriv `json:"freeTrialPrivilege"`
}

type neSong struct {
	ID          int64        `json:"id"`
	Name        string       `json:"name"`
	Ar          []neArtist   `json:"ar"`
	Al          *neAlbum     `json:"al"`
	Dt          int64        `json:"dt"`
	Fee         int          `json:"fee"`
	No          int          `json:"no"`
	Cd          string       `json:"cd"`
	PublishTime int64        `json:"publishTime"`
	Privilege   *nePrivilege `json:"privilege"`
	// legacy shape (some list endpoints)
	Artists  []neArtist `json:"artists"`
	Album    *neAlbum   `json:"album"`
	Duration int64      `json:"duration"`
}

type nePlaylist struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	CoverImgURL     string `json:"coverImgUrl"`
	PicURL          string `json:"picUrl"`
	Description     string `json:"description"`
	TrackCount      int    `json:"trackCount"`
	PlayCount       int64  `json:"playCount"`
	Playcount       int64  `json:"playcount"`
	Copywriter      string `json:"copywriter"`
	UpdateFrequency string `json:"updateFrequency"`
	SpecialType     int    `json:"specialType"`
	Subscribed      bool   `json:"subscribed"`
	Creator         *struct {
		UserID   int64  `json:"userId"`
		Nickname string `json:"nickname"`
	} `json:"creator"`
	Tracks   []neSong `json:"tracks"`
	TrackIDs []struct {
		ID int64 `json:"id"`
	} `json:"trackIds"`
}

var levelQuality = map[string]online.Quality{
	"standard": online.QStandard, "higher": online.QHigh, "exhigh": online.QHigh,
	"lossless": online.QLossless, "hires": online.QHiRes, "jyeffect": online.QHiRes, "sky": online.QHiRes, "jymaster": online.QHiRes,
}

var qualityLevel = map[online.Quality]string{
	online.QStandard: "standard", online.QHigh: "exhigh", online.QLossless: "lossless", online.QHiRes: "hires",
}

// entitlement maps a privilege to what this account can play.
//
//	st < 0                       → removed / region-blocked: unavailable
//	pl > 0                       → playable at some bitrate: full
//	pl == 0 with trial privilege → VIP/paid song, preview segment only
//	pl == 0 otherwise            → unavailable
func entitlement(p *nePrivilege) online.Entitlement {
	if p == nil {
		return online.EntUnknown
	}
	switch {
	case p.St < 0:
		return online.EntUnavailable
	case p.Pl > 0:
		return online.EntFull
	case p.FreeTrialPrivilege.ResConsumable || p.FreeTrialPrivilege.UserConsumable || p.Fee == 1 || p.Fee == 4:
		return online.EntPreview
	}
	return online.EntUnavailable
}

func refs(as []neArtist) []online.Ref {
	out := make([]online.Ref, 0, len(as))
	for _, a := range as {
		r := online.Ref{Name: a.Name}
		if a.ID > 0 {
			r.ID = fid(a.ID)
		}
		out = append(out, r)
	}
	return out
}

func names(rs []online.Ref) string {
	n := make([]string, len(rs))
	for i, r := range rs {
		n[i] = r.Name
	}
	return strings.Join(n, " / ")
}

func year(ms int64) int {
	if ms <= 0 {
		return 0
	}
	return time.UnixMilli(ms).Year()
}

func mapSong(s neSong, priv map[int64]*nePrivilege) online.Track {
	ar, al, dur := s.Ar, s.Al, s.Dt
	if len(ar) == 0 {
		ar = s.Artists
	}
	if al == nil {
		al = s.Album
	}
	if dur == 0 {
		dur = s.Duration
	}
	t := online.Track{ID: fid(s.ID), Origin: origin, Title: s.Name, Artists: refs(ar), DurationMs: dur, TrackNo: s.No, Year: year(s.PublishTime)}
	t.ArtistName = names(t.Artists)
	if al != nil && al.ID > 0 {
		t.Album = &online.Ref{ID: fid(al.ID), Name: al.Name}
		t.CoverURL = al.PicURL
		if t.Year == 0 {
			t.Year = year(al.PublishTime)
		}
	}
	if d, err := strconv.Atoi(strings.TrimLeft(s.Cd, "0")); err == nil {
		t.DiscNo = d
	}
	p := s.Privilege
	if pp, ok := priv[s.ID]; ok {
		p = pp
	}
	t.Playable = entitlement(p)
	t.VIP = s.Fee == 1 || s.Fee == 4
	if p != nil {
		t.MaxQuality = levelQuality[p.MaxBrLevel]
	}
	return t
}

func privMap(ps []nePrivilege) map[int64]*nePrivilege {
	m := make(map[int64]*nePrivilege, len(ps))
	for i := range ps {
		m[ps[i].ID] = &ps[i]
	}
	return m
}

func mapSongs(ss []neSong, ps []nePrivilege) []online.Track {
	pm := privMap(ps)
	out := make([]online.Track, 0, len(ss))
	for _, s := range ss {
		out = append(out, mapSong(s, pm))
	}
	return out
}

func mapAlbum(a neAlbum) online.Album {
	as := a.Artists
	if len(as) == 0 && a.Artist != nil {
		as = []neArtist{*a.Artist}
	}
	out := online.Album{ID: fid(a.ID), Origin: origin, Title: a.Name, Artists: refs(as), CoverURL: a.PicURL,
		Year: year(a.PublishTime), TrackCount: a.Size, Description: a.Description}
	out.ArtistName = names(out.Artists)
	return out
}

func mapArtist(a neArtist) online.Artist {
	pic := a.PicURL
	if pic == "" {
		pic = a.Img1v1URL
	}
	return online.Artist{ID: fid(a.ID), Origin: origin, Name: a.Name, PictureURL: pic, Description: a.BriefDesc,
		AlbumCount: a.AlbumSize, TrackCount: a.MusicSize}
}

func mapPlaylist(p nePlaylist, kind string) online.Playlist {
	cover := p.CoverImgURL
	if cover == "" {
		cover = p.PicURL
	}
	plays := p.PlayCount
	if plays == 0 {
		plays = p.Playcount
	}
	out := online.Playlist{ID: fid(p.ID), Origin: origin, Name: p.Name, CoverURL: cover, Description: p.Description,
		TrackCount: p.TrackCount, PlayCount: plays, Kind: kind, Subtitle: p.Copywriter}
	if p.UpdateFrequency != "" {
		out.Subtitle = p.UpdateFrequency
	}
	if p.Creator != nil {
		out.Owner = p.Creator.Nickname
	}
	if p.SpecialType == 5 {
		out.Kind = "liked"
	}
	return out
}
