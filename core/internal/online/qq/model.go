package qq

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/my-name-is-alan/yimeng/core/internal/online"
)

const origin = "qq"

func fid(native string) string { return origin + ":" + native }

type qSinger struct {
	ID   int64  `json:"id"`
	Mid  string `json:"mid"`
	Name string `json:"name"`
}

// qSong is QQ Music's uniform track_info shape (search, detail, lists).
type qSong struct {
	ID     int64     `json:"id"`
	Mid    string    `json:"mid"`
	Name   string    `json:"name"`
	Title  string    `json:"title"`
	Singer []qSinger `json:"singer"`
	Album  struct {
		Mid  string `json:"mid"`
		Name string `json:"name"`
	} `json:"album"`
	Interval   int64  `json:"interval"` // seconds
	IndexCD    int    `json:"index_cd"`
	IndexAlbum int    `json:"index_album"`
	TimePublic string `json:"time_public"`
	File       struct {
		MediaMid  string `json:"media_mid"`
		Size96AAC int64  `json:"size_96aac"`
		Size128   int64  `json:"size_128mp3"`
		Size320   int64  `json:"size_320mp3"`
		SizeFlac  int64  `json:"size_flac"`
		SizeHires int64  `json:"size_hires"`
		SizeTry   int64  `json:"size_try"`
		TryBegin  int64  `json:"try_begin"`
		TryEnd    int64  `json:"try_end"`
	} `json:"file"`
	Pay struct {
		PayPlay int `json:"pay_play"`
	} `json:"pay"`
	VS []string `json:"vs"`
}

func albumCover(mid string, size int) string {
	if mid == "" {
		return ""
	}
	return fmt.Sprintf("https://y.gtimg.cn/music/photo_new/T002R%dx%dM000%s.jpg", size, size, mid)
}

func singerPic(mid string, size int) string {
	if mid == "" {
		return ""
	}
	return fmt.Sprintf("https://y.gtimg.cn/music/photo_new/T001R%dx%dM000%s.jpg", size, size, mid)
}

func https(u string) string { return strings.Replace(u, "http://", "https://", 1) }

func (s qSong) trialFile() string {
	if s.File.SizeTry > 0 && len(s.VS) > 0 && s.VS[0] != "" {
		return "RS02" + s.VS[0] + ".mp3"
	}
	return ""
}

func mapSong(s qSong, vip bool) (online.Track, bool) {
	if s.Mid == "" {
		return online.Track{}, false
	}
	title := s.Title
	if title == "" {
		title = s.Name
	}
	t := online.Track{ID: fid(s.Mid), Origin: origin, Title: title, DurationMs: s.Interval * 1000,
		TrackNo: s.IndexAlbum, DiscNo: s.IndexCD}
	for _, a := range s.Singer {
		r := online.Ref{Name: a.Name}
		if a.Mid != "" {
			r.ID = fid("artist-" + a.Mid)
		}
		t.Artists = append(t.Artists, r)
	}
	names := make([]string, len(t.Artists))
	for i, a := range t.Artists {
		names[i] = a.Name
	}
	t.ArtistName = strings.Join(names, " / ")
	if s.Album.Mid != "" && s.Album.Name != "" {
		t.Album = &online.Ref{ID: fid("album-" + s.Album.Mid), Name: s.Album.Name}
		t.CoverURL = albumCover(s.Album.Mid, 500)
	}
	if len(s.TimePublic) >= 4 {
		t.Year, _ = strconv.Atoi(s.TimePublic[:4])
	}
	t.VIP = s.Pay.PayPlay == 1
	switch {
	case !t.VIP || vip:
		t.Playable = online.EntFull
	case s.trialFile() != "":
		t.Playable = online.EntPreview
	default:
		t.Playable = online.EntUnavailable
	}
	switch {
	case s.File.SizeHires > 0:
		t.MaxQuality = online.QHiRes
	case s.File.SizeFlac > 0:
		t.MaxQuality = online.QLossless
	case s.File.Size320 > 0:
		t.MaxQuality = online.QHigh
	default:
		t.MaxQuality = online.QStandard
	}
	return t, true
}

func mapSongs(ss []qSong, vip bool) []online.Track {
	out := make([]online.Track, 0, len(ss))
	seen := map[string]bool{}
	for _, s := range ss {
		if t, ok := mapSong(s, vip); ok && !seen[t.ID] {
			seen[t.ID] = true
			out = append(out, t)
		}
	}
	return out
}
