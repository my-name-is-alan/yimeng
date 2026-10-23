package subsonic

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/my-name-is-alan/yimeng/core/internal/auth"
	"github.com/my-name-is-alan/yimeng/core/internal/library"
)

// Catalog is the library surface Subsonic needs.
type Catalog interface {
	ListArtists(ctx context.Context, p library.ListParams) (library.Page[library.Artist], error)
	GetArtist(ctx context.Context, id string) (library.Artist, error)
	ListAlbums(ctx context.Context, p library.ListParams) (library.Page[library.Album], error)
	GetAlbum(ctx context.Context, id string) (library.Album, error)
	ListTracks(ctx context.Context, p library.ListParams) (library.Page[library.Track], error)
	GetTrack(ctx context.Context, id string) (library.Track, error)
	AlbumCover(ctx context.Context, albumID string) (string, error)
	TrackCover(ctx context.Context, trackID string) (string, error)
	TrackFile(ctx context.Context, trackID string) (library.TrackFile, error)
	Lyrics(ctx context.Context, trackID string, fetch library.LyricsFetcher) (library.TrackLyrics, error)
}

type Playlists interface {
	ListPlaylists(ctx context.Context) ([]library.Playlist, error)
	GetPlaylist(ctx context.Context, id string, limit, offset int) (library.PlaylistPage, error)
}

// Handler serves /rest/* Subsonic endpoints.
type Handler struct {
	ScopeRequest  func(http.Handler) http.Handler
	Auth          *auth.Service
	Library       Catalog
	Playlists     Playlists
	LyricsFetcher library.LyricsFetcher
	Version       string
	Stream        http.HandlerFunc // optional: if set, stream.view delegates here after auth

	// D59 wiring (all optional).
	UserData   UserData
	WithDevice func(context.Context, auth.Device) context.Context
	Decorate   func(context.Context, []library.Track) // fills Entitlement on online tracks
	ClientIP   func(*http.Request) string
	OnAuthFail func(ctx context.Context, user, ip string, locked bool)
	Cover      func(w http.ResponseWriter, r *http.Request, kind, id string) // kind album|track|artist; gateway cover serving (image cache, sizes)
	// D60: public image-cache path ("/api/v1/images/…") and biography for an artist / album, "" if none.
	ArtistInfo func(ctx context.Context, id string) (bio, imagePath string)
	AlbumImage func(ctx context.Context, id string) (notes, imagePath string)
}

func (h *Handler) Mount(mux *http.ServeMux) {
	add := func(name, scope string, fn func(http.ResponseWriter, *http.Request, auth.Device)) {
		hf := h.withAuth(scope, fn)
		for _, m := range []string{"GET", "POST"} {
			mux.HandleFunc(m+" /rest/"+name, hf)
			mux.HandleFunc(m+" /rest/"+name+".view", hf)
		}
	}
	R, P, W := auth.ScopeRead, auth.ScopePlayback, auth.ScopeWrite
	add("ping", R, h.ping)
	add("getLicense", R, h.getLicense)
	add("getOpenSubsonicExtensions", R, h.getExtensions)
	add("getMusicFolders", R, h.getMusicFolders)
	add("getIndexes", R, h.getIndexes)
	add("getArtists", R, h.getArtists)
	add("getArtist", R, h.getArtist)
	add("getArtistInfo2", R, h.getArtistInfo2)
	add("getArtistInfo", R, h.getArtistInfo2)
	add("getAlbumInfo2", R, h.getAlbumInfo2)
	add("getAlbumInfo", R, h.getAlbumInfo2)
	add("getAlbumList2", R, h.getAlbumList2)
	add("getAlbumList", R, h.getAlbumList2)
	add("getAlbum", R, h.getAlbum)
	add("getSong", R, h.getSong)
	add("search3", R, h.search3)
	add("search2", R, h.search3)
	add("getPlaylists", R, h.getPlaylists)
	add("getPlaylist", R, h.getPlaylist)
	add("createPlaylist", W, h.createPlaylist)
	add("updatePlaylist", W, h.updatePlaylist)
	add("deletePlaylist", W, h.deletePlaylist)
	add("star", W, h.star(true))
	add("unstar", W, h.star(false))
	add("getStarred2", R, h.getStarred2)
	add("getStarred", R, h.getStarred2)
	add("scrobble", P, h.scrobble)
	add("getLyrics", R, h.getLyrics)
	add("getLyricsBySongId", R, h.getLyricsBySongID)
	add("getCoverArt", R, h.getCoverArt)
	add("getUser", R, h.getUser)
	add("savePlayQueue", P, h.savePlayQueue)
	add("getPlayQueue", P, h.getPlayQueue)
	add("stream", P, h.stream)
	add("download", P, h.stream)
}

func (h *Handler) ping(w http.ResponseWriter, r *http.Request, _ auth.Device) {
	writeOK(w, r, h.Version, nil)
}

func (h *Handler) getLicense(w http.ResponseWriter, r *http.Request, _ auth.Device) {
	writeOK(w, r, h.Version, func(e *envelope) { e.License = &license{Valid: true} })
}

func (h *Handler) getMusicFolders(w http.ResponseWriter, r *http.Request, _ auth.Device) {
	writeOK(w, r, h.Version, func(e *envelope) {
		e.MusicFolders = &musicFolders{Folder: []musicFolder{{ID: 1, Name: "Music"}}}
	})
}

func (h *Handler) getArtists(w http.ResponseWriter, r *http.Request, _ auth.Device) {
	page, err := h.Library.ListArtists(r.Context(), library.ListParams{Limit: 500, Sort: "name"})
	if err != nil {
		writeErr(w, r, 0, err.Error())
		return
	}
	writeOK(w, r, h.Version, func(e *envelope) { e.Artists = &artistsIdx{Index: groupArtists(page.Items)} })
}

func (h *Handler) getIndexes(w http.ResponseWriter, r *http.Request, _ auth.Device) {
	page, err := h.Library.ListArtists(r.Context(), library.ListParams{Limit: 500, Sort: "name"})
	if err != nil {
		writeErr(w, r, 0, err.Error())
		return
	}
	writeOK(w, r, h.Version, func(e *envelope) { e.Indexes = &indexes{Index: groupArtists(page.Items)} })
}

func groupArtists(items []library.Artist) []artistIndex {
	buckets := map[string][]artist{}
	var keys []string
	for _, a := range items {
		k := indexKey(a.Name)
		if _, ok := buckets[k]; !ok {
			keys = append(keys, k)
		}
		buckets[k] = append(buckets[k], artist{ID: a.ID, Name: a.Name, AlbumCount: a.AlbumCount, CoverArt: "ar-" + a.ID})
	}
	out := make([]artistIndex, 0, len(keys))
	for _, k := range keys {
		out = append(out, artistIndex{Name: k, Artist: buckets[k]})
	}
	return out
}

func indexKey(name string) string {
	for _, r := range strings.TrimSpace(name) {
		if unicode.IsLetter(r) {
			return strings.ToUpper(string(unicode.ToUpper(r)))
		}
		if unicode.IsDigit(r) {
			return "#"
		}
	}
	return "#"
}

func (h *Handler) getAlbumList2(w http.ResponseWriter, r *http.Request, _ auth.Device) {
	q := r.URL.Query()
	size, _ := strconv.Atoi(q.Get("size"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	if size <= 0 {
		size = 50
	}
	p := library.ListParams{Limit: size, Offset: offset, Sort: "title"}
	switch q.Get("type") {
	case "newest":
		p.Sort, p.Desc = "added", true
	case "alphabeticalByName":
		p.Sort = "title"
	case "alphabeticalByArtist":
		p.Sort = "artist"
	case "starred":
		p.Favorite = true
	default:
		p.Sort, p.Desc = "added", true
	}
	page, err := h.Library.ListAlbums(r.Context(), p)
	if err != nil {
		writeErr(w, r, 0, err.Error())
		return
	}
	albs := make([]album, 0, len(page.Items))
	for _, a := range page.Items {
		albs = append(albs, mapAlbum(a))
	}
	writeOK(w, r, h.Version, func(e *envelope) { e.AlbumList2 = &albumList2{Album: albs} })
}

func (h *Handler) getAlbum(w http.ResponseWriter, r *http.Request, _ auth.Device) {
	id := r.URL.Query().Get("id")
	a, err := h.Library.GetAlbum(r.Context(), id)
	if errors.Is(err, library.ErrNotFound) {
		writeErr(w, r, 70, "album not found")
		return
	}
	if err != nil {
		writeErr(w, r, 0, err.Error())
		return
	}
	tracks, err := h.Library.ListTracks(r.Context(), library.ListParams{AlbumID: id, Limit: 500, Sort: "track"})
	if err != nil {
		writeErr(w, r, 0, err.Error())
		return
	}
	det := &albumDetail{album: mapAlbum(a)}
	for _, t := range h.visible(r.Context(), tracks.Items) {
		det.Song = append(det.Song, mapSong(t))
	}
	writeOK(w, r, h.Version, func(e *envelope) { e.Album = det })
}

func (h *Handler) getSong(w http.ResponseWriter, r *http.Request, _ auth.Device) {
	id := r.URL.Query().Get("id")
	t, err := h.Library.GetTrack(r.Context(), id)
	if errors.Is(err, library.ErrNotFound) {
		writeErr(w, r, 70, "song not found")
		return
	}
	if err != nil {
		writeErr(w, r, 0, err.Error())
		return
	}
	if v := h.visible(r.Context(), []library.Track{t}); len(v) == 0 {
		writeErr(w, r, 70, "song not available on this server (preview-only or unavailable)")
		return
	}
	writeOK(w, r, h.Version, func(e *envelope) { s := mapSong(t); e.Song = &s })
}

func (h *Handler) search3(w http.ResponseWriter, r *http.Request, _ auth.Device) {
	q := r.URL.Query().Get("query")
	if q == "" {
		writeErr(w, r, 10, "query required")
		return
	}
	nArtist, _ := strconv.Atoi(r.URL.Query().Get("artistCount"))
	nAlbum, _ := strconv.Atoi(r.URL.Query().Get("albumCount"))
	nSong, _ := strconv.Atoi(r.URL.Query().Get("songCount"))
	if nArtist <= 0 {
		nArtist = 20
	}
	if nAlbum <= 0 {
		nAlbum = 20
	}
	if nSong <= 0 {
		nSong = 20
	}
	ctx := r.Context()
	ar, _ := h.Library.ListArtists(ctx, library.ListParams{Q: q, Limit: nArtist})
	al, _ := h.Library.ListAlbums(ctx, library.ListParams{Q: q, Limit: nAlbum})
	tr, _ := h.Library.ListTracks(ctx, library.ListParams{Q: q, Limit: nSong})
	res := &searchResult3{}
	for _, a := range ar.Items {
		res.Artist = append(res.Artist, artist{ID: a.ID, Name: a.Name, AlbumCount: a.AlbumCount, CoverArt: "ar-" + a.ID})
	}
	for _, a := range al.Items {
		res.Album = append(res.Album, mapAlbum(a))
	}
	for _, t := range h.visible(ctx, tr.Items) {
		res.Song = append(res.Song, mapSong(t))
	}
	writeOK(w, r, h.Version, func(e *envelope) { e.SearchResult3 = res })
}

func (h *Handler) getPlaylists(w http.ResponseWriter, r *http.Request, _ auth.Device) {
	if h.Playlists == nil {
		writeOK(w, r, h.Version, func(e *envelope) { e.Playlists = &playlists{} })
		return
	}
	list, err := h.Playlists.ListPlaylists(r.Context())
	if err != nil {
		writeErr(w, r, 0, err.Error())
		return
	}
	out := &playlists{}
	for _, p := range list {
		out.Playlist = append(out.Playlist, playlist{
			ID: p.ID, Name: p.Name, SongCount: p.TrackCount,
			Duration: int(p.DurationMs / 1000), Public: false, Owner: h.ownerName(r.Context()),
		})
	}
	writeOK(w, r, h.Version, func(e *envelope) { e.Playlists = out })
}

func (h *Handler) getPlaylist(w http.ResponseWriter, r *http.Request, _ auth.Device) {
	if h.Playlists == nil {
		writeErr(w, r, 70, "playlist not found")
		return
	}
	id := r.URL.Query().Get("id")
	page, err := h.Playlists.GetPlaylist(r.Context(), id, 500, 0)
	if errors.Is(err, library.ErrNotFound) {
		writeErr(w, r, 70, "playlist not found")
		return
	}
	if err != nil {
		writeErr(w, r, 0, err.Error())
		return
	}
	det := &playlistDetail{playlist: playlist{
		ID: page.ID, Name: page.Name, SongCount: page.TrackCount,
		Duration: int(page.DurationMs / 1000), Public: false, Owner: h.ownerName(r.Context()),
	}}
	for _, t := range h.visible(r.Context(), page.Tracks.Items) {
		det.Entry = append(det.Entry, mapSong(t))
	}
	writeOK(w, r, h.Version, func(e *envelope) { e.Playlist = det })
}

func (h *Handler) getLyrics(w http.ResponseWriter, r *http.Request, _ auth.Device) {
	artist, title := r.URL.Query().Get("artist"), r.URL.Query().Get("title")
	// Prefer artist+title search via track list; fallback empty.
	page, err := h.Library.ListTracks(r.Context(), library.ListParams{Q: title, Limit: 20})
	if err != nil {
		writeErr(w, r, 0, err.Error())
		return
	}
	for _, t := range page.Items {
		if title != "" && !strings.EqualFold(t.Title, title) {
			continue
		}
		if artist != "" && !strings.Contains(strings.ToLower(t.ArtistName), strings.ToLower(artist)) {
			continue
		}
		ly, err := h.Library.Lyrics(r.Context(), t.ID, h.LyricsFetcher)
		if err != nil {
			continue
		}
		text := ly.Text
		if text == "" && len(ly.Lines) > 0 {
			var b strings.Builder
			for _, line := range ly.Lines {
				b.WriteString(line.Text)
				b.WriteByte('\n')
			}
			text = b.String()
		}
		writeOK(w, r, h.Version, func(e *envelope) {
			e.Lyrics = &lyrics{Artist: t.ArtistName, Title: t.Title, Value: text}
		})
		return
	}
	writeOK(w, r, h.Version, func(e *envelope) { e.Lyrics = &lyrics{Artist: artist, Title: title} })
}

func (h *Handler) getCoverArt(w http.ResponseWriter, r *http.Request, _ auth.Device) {
	id := r.URL.Query().Get("id")
	if h.Cover != nil && id != "" {
		kind, raw := "album", id
		switch {
		case strings.HasPrefix(id, "ar-"):
			kind, raw = "artist", strings.TrimPrefix(id, "ar-")
		case strings.HasPrefix(id, "al-"):
			raw = strings.TrimPrefix(id, "al-")
		case strings.HasPrefix(id, "tr-"):
			kind, raw = "track", strings.TrimPrefix(id, "tr-")
		default:
			if p, err := h.Library.AlbumCover(r.Context(), id); err != nil || p == "" {
				kind = "track"
			}
		}
		h.Cover(w, r, kind, raw)
		return
	}
	var path string
	var err error
	switch {
	case strings.HasPrefix(id, "ar-"):
		// no per-artist art yet
		writeErr(w, r, 70, "cover not found")
		return
	case strings.HasPrefix(id, "al-"):
		path, err = h.Library.AlbumCover(r.Context(), strings.TrimPrefix(id, "al-"))
	case strings.HasPrefix(id, "tr-"):
		path, err = h.Library.TrackCover(r.Context(), strings.TrimPrefix(id, "tr-"))
	default:
		// try album then track
		path, err = h.Library.AlbumCover(r.Context(), id)
		if err != nil {
			path, err = h.Library.TrackCover(r.Context(), id)
		}
	}
	if err != nil || path == "" {
		writeErr(w, r, 70, "cover not found")
		return
	}
	fh, err := os.Open(path)
	if err != nil {
		writeErr(w, r, 70, "cover not found")
		return
	}
	defer fh.Close()
	st, _ := fh.Stat()
	http.ServeContent(w, r, st.Name(), st.ModTime(), fh)
}

func (h *Handler) stream(w http.ResponseWriter, r *http.Request, _ auth.Device) {
	id := r.URL.Query().Get("id")
	if id == "" {
		writeErr(w, r, 10, "id required")
		return
	}
	if h.Stream != nil {
		// Rewrite path so the gateway stream handler sees /api/stream/{id}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/api/stream/" + id
		r2.SetPathValue("id", id)
		if r2.Method != http.MethodHead {
			r2.Method = http.MethodGet // formPost: the upstream fetch must be a GET
		}
		r2.Body = http.NoBody
		r2.Header.Set("X-Yimeng-Full-Only", "1") // never hand a preview clip to a Subsonic client
		if q := r.URL.Query(); q.Get("format") == "raw" || q.Get("format") == "" {
			q.Del("format")
			r2.URL.RawQuery = q.Encode()
		}
		h.Stream(w, r2)
		return
	}
	f, err := h.Library.TrackFile(r.Context(), id)
	if errors.Is(err, library.ErrNotFound) {
		writeErr(w, r, 70, "song not found")
		return
	}
	if err != nil {
		writeErr(w, r, 0, err.Error())
		return
	}
	fh, err := os.Open(f.Path)
	if err != nil {
		writeErr(w, r, 70, "song not found")
		return
	}
	defer fh.Close()
	http.ServeContent(w, r, "", time.Unix(0, f.Mtime), fh)
}

func mapAlbum(a library.Album) album {
	cover := ""
	if a.HasCover {
		cover = a.ID
	}
	artistID := ""
	if len(a.Artists) > 0 {
		artistID = a.Artists[0].ID
	}
	return album{
		ID: a.ID, Name: a.Title, Artist: a.ArtistName, ArtistID: artistID,
		CoverArt: cover, SongCount: a.TrackCount, Duration: int(a.DurationMs / 1000),
		Year: a.Year, Genre: a.Genre,
	}
}

func mapSong(t library.Track) song {
	artistID := ""
	if len(t.Artists) > 0 {
		artistID = t.Artists[0].ID
	}
	suf := t.Format
	ct := ""
	switch strings.ToLower(suf) {
	case "flac":
		ct = "audio/flac"
	case "mp3":
		ct = "audio/mpeg"
	case "aac", "m4a":
		ct = "audio/mp4"
	case "opus", "ogg":
		ct = "audio/ogg"
	}
	br := 0
	sz := int64(0)
	if t.Stream != nil {
		br = t.Stream.BitrateKbps
		sz = t.Stream.SizeBytes
	}
	return song{
		ID: t.ID, Parent: t.AlbumID, Title: t.Title, Album: t.AlbumTitle, Artist: t.ArtistName,
		AlbumID: t.AlbumID, ArtistID: artistID, CoverArt: "tr-" + t.ID,
		Duration: int(t.DurationMs / 1000), Track: t.TrackNo, DiscNumber: t.DiscNo,
		Year: t.Year, Genre: t.Genre, Size: sz, ContentType: ct, Suffix: suf, BitRate: br,
		Type: "music", IsDir: false,
	}
}
