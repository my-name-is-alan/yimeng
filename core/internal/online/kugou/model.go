package kugou

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/my-name-is-alan/yimeng/core/internal/online"
)

const origin = "kugou"

// Kugou's endpoints return a dozen differently-shaped song objects; decoding
// them into generic maps and reading by path keeps the mapping in one place.
type obj = map[string]any

func get(m obj, path string) any {
	var cur any = m
	for _, k := range strings.Split(path, ".") {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[k]
	}
	return cur
}

// str returns the first non-empty value among paths, as a string.
func str(m obj, paths ...string) string {
	for _, p := range paths {
		switch v := get(m, p).(type) {
		case string:
			if v != "" {
				return v
			}
		case float64:
			if v != 0 {
				return strconv.FormatFloat(v, 'f', -1, 64)
			}
		case json.Number:
			if v != "" && v != "0" {
				return v.String()
			}
		}
	}
	return ""
}

func num(m obj, paths ...string) int64 {
	s := str(m, paths...)
	if s == "" {
		return 0
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n
	}
	f, _ := strconv.ParseFloat(s, 64)
	return int64(f)
}

func list(m obj, path string) []obj {
	arr, _ := get(m, path).([]any)
	out := make([]obj, 0, len(arr))
	for _, x := range arr {
		if o, ok := x.(map[string]any); ok {
			out = append(out, o)
		}
	}
	return out
}

// img fills Kugou's "{size}" placeholder and upgrades to https.
func img(u string, size int) string {
	if u == "" {
		return ""
	}
	u = strings.ReplaceAll(u, "{size}", strconv.Itoa(size))
	return strings.Replace(u, "http://", "https://", 1)
}

// Native track id: "<hash>-<album_id>-<album_audio_id>" (hash lowercase hex).
func trackNative(hash, album, mix string) string {
	if album == "" {
		album = "0"
	}
	if mix == "" {
		mix = "0"
	}
	return strings.ToLower(hash) + "-" + album + "-" + mix
}

type trackRef struct{ Hash, Album, Mix string }

func parseTrack(id string) (trackRef, error) {
	parts := strings.Split(id, "-")
	if len(parts) == 0 || len(parts[0]) != 32 {
		return trackRef{}, fmt.Errorf("%w: kugou track id %q", online.ErrBadInput, id)
	}
	r := trackRef{Hash: strings.ToLower(parts[0]), Album: "0", Mix: "0"}
	if len(parts) > 1 && parts[1] != "" {
		r.Album = parts[1]
	}
	if len(parts) > 2 && parts[2] != "" {
		r.Mix = parts[2]
	}
	return r, nil
}

func fid(native string) string { return origin + ":" + native }

func splitFileName(name string) (artist, title string) {
	if a, t, ok := strings.Cut(name, " - "); ok {
		return strings.TrimSpace(a), strings.TrimSpace(t)
	}
	return "", strings.TrimSpace(name)
}

func artistRefs(m obj, author string) []online.Ref {
	var out []online.Ref
	for _, key := range []string{"Singers", "singerinfo", "authors"} {
		for _, s := range list(m, key) {
			name := str(s, "name", "author_name")
			if name == "" {
				continue
			}
			r := online.Ref{Name: name}
			if id := str(s, "id", "author_id"); id != "" && id != "0" {
				r.ID = fid("artist-" + id)
			}
			out = append(out, r)
		}
		if len(out) > 0 {
			return out
		}
	}
	for _, n := range strings.FieldsFunc(author, func(r rune) bool { return r == '、' || r == ',' || r == '/' }) {
		out = append(out, online.Ref{Name: strings.TrimSpace(n)})
	}
	return out
}

func joinNames(rs []online.Ref) string {
	n := make([]string, len(rs))
	for i, r := range rs {
		n[i] = r.Name
	}
	return strings.Join(n, " / ")
}

// mapSong understands search, everyday, rank, playlist, album, artist and
// privilege shapes. vip tells whether the logged-in account has VIP.
func mapSong(m obj, vip bool) (online.Track, bool) {
	hash := str(m, "FileHash", "hash", "audio_info.hash_128", "audio_info.hash", "hash_128")
	if hash == "" {
		return online.Track{}, false
	}
	album := str(m, "AlbumID", "album_id", "base.album_id", "albuminfo.id")
	mix := str(m, "MixSongID", "mixsongid", "add_mixsongid", "album_audio_id", "base.album_audio_id")
	title := str(m, "SongName", "songname", "audio_name", "base.audio_name", "OriSongName")
	author := str(m, "SingerName", "author_name", "singername", "base.author_name")
	if title == "" || strings.Contains(title, " - ") && author == "" {
		a, t := splitFileName(str(m, "name", "FileName", "filename", "songname"))
		if title == "" || author == "" {
			title = t
		}
		if author == "" {
			author = a
		}
	}
	arts := artistRefs(m, author)
	t := online.Track{ID: fid(trackNative(hash, album, mix)), Origin: origin, Title: title, Artists: arts, ArtistName: joinNames(arts)}
	if t.ArtistName == "" {
		t.ArtistName = author
	}
	if an := str(m, "AlbumName", "album_name", "albumname", "album_info.album_name", "albuminfo.name"); an != "" {
		t.Album = &online.Ref{Name: an}
		if album != "" && album != "0" {
			t.Album.ID = fid("album-" + album)
		}
	}
	t.CoverURL = img(str(m, "Image", "sizable_cover", "cover", "album_info.cover", "album_info.sizable_cover", "info.image", "trans_param.union_cover"), 480)
	if ms := num(m, "timelen", "timelength", "audio_info.duration_128", "audio_info.duration", "info.duration"); ms > 0 {
		t.DurationMs = ms
	} else if s := num(m, "Duration", "time_length"); s > 0 {
		t.DurationMs = s * 1000
	}
	if d := str(m, "PublishDate", "publish_date"); len(d) >= 4 {
		t.Year, _ = strconv.Atoi(d[:4])
	}
	if n := num(m, "extend.sort"); n > 0 {
		t.TrackNo = int(n)
	}
	if n := num(m, "extend.disc"); n > 0 {
		t.DiscNo = int(n)
	}
	priv := num(m, "Privilege", "privilege", "copyright.privilege", "privilege_download.privilege", "privilege_128")
	pay := num(m, "PayType", "pay_type", "deprecated.pay_type")
	t.VIP = priv == 10 || pay == 3
	switch {
	case !t.VIP || vip:
		t.Playable = online.EntFull
	default:
		t.Playable = online.EntPreview
	}
	switch {
	case str(m, "ResFileHash", "hash_high", "audio_info.hash_high") != "":
		t.MaxQuality = online.QHiRes
	case str(m, "SQFileHash", "hash_flac", "audio_info.hash_flac") != "":
		t.MaxQuality = online.QLossless
	case str(m, "HQFileHash", "hash_320", "audio_info.hash_320") != "":
		t.MaxQuality = online.QHigh
	default:
		t.MaxQuality = online.QStandard
	}
	return t, true
}

func mapSongs(ms []obj, vip bool) []online.Track {
	out := make([]online.Track, 0, len(ms))
	seen := map[string]bool{}
	for _, m := range ms {
		if t, ok := mapSong(m, vip); ok && !seen[t.ID] {
			seen[t.ID] = true
			out = append(out, t)
		}
	}
	return out
}

func mapPlaylist(m obj) online.Playlist {
	id := str(m, "global_collection_id", "gid", "list_create_gid")
	p := online.Playlist{ID: fid("pl-" + id), Origin: origin, Kind: "playlist",
		Name:        str(m, "specialname", "name"),
		CoverURL:    img(str(m, "flexible_cover", "imgurl", "img", "pic"), 400),
		Description: str(m, "intro"),
		TrackCount:  int(num(m, "song_count", "count", "songcount")),
		Owner:       str(m, "nickname", "list_create_username"),
		PlayCount:   num(m, "play_count", "total_play_count", "collectcount"),
	}
	return p
}
