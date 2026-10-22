package subsonic

// OpenSubsonic additions (D59): extensions, artist detail, playlist writes,
// stars, scrobble, structured lyrics, user and play queue.

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/auth"
	"github.com/my-name-is-alan/yimeng/core/internal/library"
	"github.com/my-name-is-alan/yimeng/core/internal/owner"
	"github.com/my-name-is-alan/yimeng/core/internal/userdata"
)

// UserData is the per-account store (favorites, plays, prefs).
type UserData interface {
	SetFavorite(ctx context.Context, typ, id string, fav bool) error
	Favorites(ctx context.Context, typ string, limit, offset int) ([]userdata.Favorite, error)
	Record(ctx context.Context, p userdata.Play) error
	GetPref(ctx context.Context, owner, key string, v any) (bool, error)
	SetPref(ctx context.Context, owner, key string, v any) error
}

type playlistWriter interface {
	CreatePlaylist(ctx context.Context, in library.PlaylistInput) (library.Playlist, error)
	UpdatePlaylist(ctx context.Context, id string, in library.PlaylistInput) (library.Playlist, error)
	DeletePlaylist(ctx context.Context, id string) error
}

// visible drops online tracks a Subsonic client could not play in full:
// preview-only and unavailable ones (no DRM bypass, no silent 30 s clips).
func (h *Handler) visible(ctx context.Context, ts []library.Track) []library.Track {
	if len(ts) == 0 {
		return ts
	}
	if h.Decorate != nil {
		h.Decorate(ctx, ts)
	}
	out := ts[:0:0]
	for _, t := range ts {
		if library.IsExternalID(t.ID) && (t.Entitlement == "preview" || t.Entitlement == "unavailable") {
			continue
		}
		out = append(out, t)
	}
	return out
}

func (h *Handler) ownerName(ctx context.Context) string {
	if o := owner.From(ctx); o.ID != "" && h.Auth != nil {
		if u, err := h.Auth.UserByID(ctx, o.ID); err == nil {
			return u.Username
		}
	}
	return "yimeng"
}

func (h *Handler) getExtensions(w http.ResponseWriter, r *http.Request, _ auth.Device) {
	writeOK(w, r, h.Version, func(e *envelope) {
		e.Extensions = []extension{
			{Name: "apiKeyAuthentication", Versions: []int{1}},
			{Name: "formPost", Versions: []int{1}},
			{Name: "songLyrics", Versions: []int{1}},
		}
	})
}

func (h *Handler) getArtist(w http.ResponseWriter, r *http.Request, _ auth.Device) {
	id := r.URL.Query().Get("id")
	a, err := h.Library.GetArtist(r.Context(), id)
	if errors.Is(err, library.ErrNotFound) {
		writeErr(w, r, 70, "artist not found")
		return
	}
	if err != nil {
		writeErr(w, r, 0, "internal error")
		return
	}
	al, err := h.Library.ListAlbums(r.Context(), library.ListParams{ArtistID: id, Limit: 500, Sort: "year"})
	if err != nil {
		writeErr(w, r, 0, "internal error")
		return
	}
	det := &artistDetail{artist: artist{ID: a.ID, Name: a.Name, AlbumCount: a.AlbumCount, CoverArt: "ar-" + a.ID}}
	if h.ArtistInfo != nil {
		if _, img := h.ArtistInfo(r.Context(), a.ID); img != "" {
			det.ArtistImageURL = absURL(r, img, 600)
		}
	}
	for _, x := range al.Items {
		det.Album = append(det.Album, mapAlbum(x))
	}
	writeOK(w, r, h.Version, func(e *envelope) { e.Artist = det })
}

func (h *Handler) pw(w http.ResponseWriter, r *http.Request) playlistWriter {
	pw, ok := h.Playlists.(playlistWriter)
	if !ok {
		writeErr(w, r, 0, "playlists are read-only on this server")
		return nil
	}
	return pw
}

func (h *Handler) writePlaylist(w http.ResponseWriter, r *http.Request, id string) {
	page, err := h.Playlists.GetPlaylist(r.Context(), id, 10000, 0)
	if err != nil {
		writeErr(w, r, 70, "playlist not found")
		return
	}
	det := &playlistDetail{playlist: playlist{ID: page.ID, Name: page.Name, SongCount: page.TrackCount,
		Duration: int(page.DurationMs / 1000), Owner: h.ownerName(r.Context())}}
	for _, t := range h.visible(r.Context(), page.Tracks.Items) {
		det.Entry = append(det.Entry, mapSong(t))
	}
	writeOK(w, r, h.Version, func(e *envelope) { e.Playlist = det })
}

// createPlaylist: name + songId… creates; playlistId + songId… replaces.
func (h *Handler) createPlaylist(w http.ResponseWriter, r *http.Request, _ auth.Device) {
	pw := h.pw(w, r)
	if pw == nil {
		return
	}
	q := r.URL.Query()
	ids := q["songId"]
	if ids == nil {
		ids = []string{}
	}
	if pid := q.Get("playlistId"); pid != "" {
		in := library.PlaylistInput{TrackIDs: ids}
		if n := q.Get("name"); n != "" {
			in.Name = &n
		}
		if _, err := pw.UpdatePlaylist(r.Context(), pid, in); err != nil {
			h.plErr(w, r, err)
			return
		}
		h.writePlaylist(w, r, pid)
		return
	}
	name := q.Get("name")
	if name == "" {
		writeErr(w, r, 10, "name or playlistId required")
		return
	}
	p, err := pw.CreatePlaylist(r.Context(), library.PlaylistInput{Name: &name, TrackIDs: ids})
	if err != nil {
		h.plErr(w, r, err)
		return
	}
	h.writePlaylist(w, r, p.ID)
}

func (h *Handler) plErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, library.ErrNotFound):
		writeErr(w, r, 70, "playlist or song not found")
	case errors.Is(err, library.ErrBadInput), errors.Is(err, library.ErrSmartReadOnly):
		writeErr(w, r, 10, err.Error())
	default:
		writeErr(w, r, 0, "internal error")
	}
}

func (h *Handler) updatePlaylist(w http.ResponseWriter, r *http.Request, _ auth.Device) {
	pw := h.pw(w, r)
	if pw == nil {
		return
	}
	q := r.URL.Query()
	id := q.Get("playlistId")
	page, err := h.Playlists.GetPlaylist(r.Context(), id, 10000, 0)
	if err != nil {
		writeErr(w, r, 70, "playlist not found")
		return
	}
	in := library.PlaylistInput{}
	if q.Has("name") {
		n := q.Get("name")
		in.Name = &n
	}
	if q.Has("comment") {
		c := q.Get("comment")
		in.Comment = &c
	}
	if q.Has("songIdToAdd") || q.Has("songIndexToRemove") {
		drop := map[int]bool{}
		for _, v := range q["songIndexToRemove"] {
			i, err := strconv.Atoi(v)
			if err != nil || i < 0 || i >= len(page.Tracks.Items) {
				writeErr(w, r, 10, "songIndexToRemove out of range")
				return
			}
			drop[i] = true
		}
		ids := []string{}
		for i, t := range page.Tracks.Items {
			if !drop[i] {
				ids = append(ids, t.ID)
			}
		}
		in.TrackIDs = append(ids, q["songIdToAdd"]...)
	}
	if _, err := pw.UpdatePlaylist(r.Context(), id, in); err != nil {
		h.plErr(w, r, err)
		return
	}
	writeOK(w, r, h.Version, nil)
}

func (h *Handler) deletePlaylist(w http.ResponseWriter, r *http.Request, _ auth.Device) {
	pw := h.pw(w, r)
	if pw == nil {
		return
	}
	if err := pw.DeletePlaylist(r.Context(), r.URL.Query().Get("id")); err != nil {
		h.plErr(w, r, err)
		return
	}
	writeOK(w, r, h.Version, nil)
}

func (h *Handler) needUD(w http.ResponseWriter, r *http.Request) bool {
	if h.UserData == nil {
		writeErr(w, r, 0, "user data unavailable")
		return false
	}
	return true
}

func (h *Handler) star(fav bool) func(http.ResponseWriter, *http.Request, auth.Device) {
	return func(w http.ResponseWriter, r *http.Request, _ auth.Device) {
		if !h.needUD(w, r) {
			return
		}
		q := r.URL.Query()
		for typ, key := range map[string]string{"track": "id", "album": "albumId", "artist": "artistId"} {
			for _, id := range q[key] {
				if err := h.UserData.SetFavorite(r.Context(), typ, id, fav); err != nil {
					if errors.Is(err, userdata.ErrNotFound) && typ == "track" {
						// Subsonic clients send albums/artists as id= too
						if h.UserData.SetFavorite(r.Context(), "album", id, fav) == nil ||
							h.UserData.SetFavorite(r.Context(), "artist", id, fav) == nil {
							continue
						}
					}
					writeErr(w, r, 70, "item not found: "+id)
					return
				}
			}
		}
		writeOK(w, r, h.Version, nil)
	}
}

func (h *Handler) getStarred2(w http.ResponseWriter, r *http.Request, _ auth.Device) {
	if !h.needUD(w, r) {
		return
	}
	ctx := r.Context()
	out := &starred2{Artist: []artist{}, Album: []album{}, Song: []song{}}
	if favs, err := h.UserData.Favorites(ctx, "artist", 500, 0); err == nil {
		for _, f := range favs {
			if a, err := h.Library.GetArtist(ctx, f.ID); err == nil {
				out.Artist = append(out.Artist, artist{ID: a.ID, Name: a.Name, AlbumCount: a.AlbumCount, CoverArt: "ar-" + a.ID, Starred: f.CreatedAt.UTC().Format(time.RFC3339)})
			}
		}
	}
	if favs, err := h.UserData.Favorites(ctx, "album", 500, 0); err == nil {
		for _, f := range favs {
			if a, err := h.Library.GetAlbum(ctx, f.ID); err == nil {
				m := mapAlbum(a)
				m.Starred = f.CreatedAt.UTC().Format(time.RFC3339)
				out.Album = append(out.Album, m)
			}
		}
	}
	if favs, err := h.UserData.Favorites(ctx, "track", 1000, 0); err == nil {
		var ts []library.Track
		when := map[string]string{}
		for _, f := range favs {
			if t, err := h.Library.GetTrack(ctx, f.ID); err == nil {
				ts = append(ts, t)
				when[t.ID] = f.CreatedAt.UTC().Format(time.RFC3339)
			}
		}
		for _, t := range h.visible(ctx, ts) {
			s := mapSong(t)
			s.Starred = when[t.ID]
			out.Song = append(out.Song, s)
		}
	}
	writeOK(w, r, h.Version, func(e *envelope) { e.Starred2 = out })
}

// scrobble: submission=false (now playing) is accepted and ignored;
// submission=true records a completed play per id (time in ms optional).
func (h *Handler) scrobble(w http.ResponseWriter, r *http.Request, dev auth.Device) {
	if !h.needUD(w, r) {
		return
	}
	q := r.URL.Query()
	if q.Get("submission") == "false" {
		writeOK(w, r, h.Version, nil)
		return
	}
	times := q["time"]
	for i, id := range q["id"] {
		t, err := h.Library.GetTrack(r.Context(), id)
		if err != nil {
			writeErr(w, r, 70, "song not found: "+id)
			return
		}
		at := time.Now()
		if i < len(times) {
			if ms, err := strconv.ParseInt(times[i], 10, 64); err == nil && ms > 0 {
				at = time.UnixMilli(ms)
			}
		}
		if err := h.UserData.Record(r.Context(), userdata.Play{TrackID: t.ID, PlayedAt: at,
			DurationMs: t.DurationMs, Completed: true, Source: "subsonic"}); err != nil {
			writeErr(w, r, 0, "could not record play")
			return
		}
	}
	writeOK(w, r, h.Version, nil)
}

func (h *Handler) getLyricsBySongID(w http.ResponseWriter, r *http.Request, _ auth.Device) {
	id := r.URL.Query().Get("id")
	t, err := h.Library.GetTrack(r.Context(), id)
	if err != nil {
		writeErr(w, r, 70, "song not found")
		return
	}
	out := &lyricsList{StructuredLyrics: []structuredLyrics{}}
	if ly, err := h.Library.Lyrics(r.Context(), id, h.LyricsFetcher); err == nil && (len(ly.Lines) > 0 || ly.Text != "") {
		sl := structuredLyrics{Lang: "xxx", Synced: ly.Synced, DisplayArtist: t.ArtistName, DisplayTitle: t.Title, Line: []lyricLine{}}
		if len(ly.Lines) > 0 {
			for _, l := range ly.Lines {
				ll := lyricLine{Value: l.Text}
				if ly.Synced {
					ms := l.TimeMs
					ll.Start = &ms
				}
				sl.Line = append(sl.Line, ll)
			}
		} else {
			sl.Line = append(sl.Line, lyricLine{Value: ly.Text})
		}
		out.StructuredLyrics = append(out.StructuredLyrics, sl)
	}
	writeOK(w, r, h.Version, func(e *envelope) { e.LyricsList = out })
}

func (h *Handler) getUser(w http.ResponseWriter, r *http.Request, dev auth.Device) {
	name := h.ownerName(r.Context())
	if u := r.URL.Query().Get("username"); u != "" && u != name {
		writeErr(w, r, 50, "you may only read your own user")
		return
	}
	can := func(s string) bool { return dev.Via == "" || dev.HasScope(s) }
	writeOK(w, r, h.Version, func(e *envelope) {
		e.User = &user{Username: name, ScrobblingEnabled: true, AdminRole: dev.Admin,
			SettingsRole: false, DownloadRole: can(auth.ScopePlayback), StreamRole: can(auth.ScopePlayback),
			PlaylistRole: can(auth.ScopeWrite), CoverArtRole: true, Folder: []int{1}}
	})
}

type savedQueue struct {
	IDs       []string  `json:"ids"`
	Current   string    `json:"current"`
	Position  int64     `json:"position"`
	Changed   time.Time `json:"changed"`
	ChangedBy string    `json:"changed_by"`
}

const queuePref = "subsonic.playqueue"

func (h *Handler) savePlayQueue(w http.ResponseWriter, r *http.Request, _ auth.Device) {
	if !h.needUD(w, r) {
		return
	}
	q := r.URL.Query()
	ids := q["id"]
	if len(ids) > 2000 {
		ids = ids[:2000]
	}
	pos, _ := strconv.ParseInt(q.Get("position"), 10, 64)
	sq := savedQueue{IDs: ids, Current: q.Get("current"), Position: pos, Changed: time.Now().UTC(), ChangedBy: truncate(q.Get("c"), 64)}
	if err := h.UserData.SetPref(r.Context(), owner.From(r.Context()).ID, queuePref, sq); err != nil {
		writeErr(w, r, 0, "could not save play queue")
		return
	}
	writeOK(w, r, h.Version, nil)
}

func (h *Handler) getPlayQueue(w http.ResponseWriter, r *http.Request, _ auth.Device) {
	if !h.needUD(w, r) {
		return
	}
	var sq savedQueue
	ok, err := h.UserData.GetPref(r.Context(), owner.From(r.Context()).ID, queuePref, &sq)
	if err != nil {
		writeErr(w, r, 0, "could not load play queue")
		return
	}
	if !ok {
		writeOK(w, r, h.Version, nil) // spec: no playQueue element when none saved
		return
	}
	var ts []library.Track
	for _, id := range sq.IDs {
		if t, err := h.Library.GetTrack(r.Context(), id); err == nil {
			ts = append(ts, t)
		}
	}
	pq := &playQueue{Current: sq.Current, Position: sq.Position, Username: h.ownerName(r.Context()),
		Changed: sq.Changed.Format(time.RFC3339), ChangedBy: sq.ChangedBy, Entry: []song{}}
	for _, t := range h.visible(r.Context(), ts) {
		pq.Entry = append(pq.Entry, mapSong(t))
	}
	writeOK(w, r, h.Version, func(e *envelope) { e.PlayQueue = pq })
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// absURL turns a public image-cache path into an absolute URL for clients.
func absURL(r *http.Request, path string, size int) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host + path + "?size=" + strconv.Itoa(size)
}

func (h *Handler) getArtistInfo2(w http.ResponseWriter, r *http.Request, _ auth.Device) {
	id := strings.TrimPrefix(r.URL.Query().Get("id"), "ar-")
	a, err := h.Library.GetArtist(r.Context(), id)
	if err != nil {
		writeErr(w, r, 70, "artist not found")
		return
	}
	info := &artistInfo{MusicBrainzID: a.MBID}
	if h.ArtistInfo != nil {
		bio, img := h.ArtistInfo(r.Context(), a.ID)
		info.Biography = bio
		if img != "" {
			info.SmallImageURL, info.MediumImageURL, info.LargeImageURL = absURL(r, img, 120), absURL(r, img, 300), absURL(r, img, 1200)
		}
	}
	writeOK(w, r, h.Version, func(e *envelope) { e.ArtistInfo2 = info })
}

func (h *Handler) getAlbumInfo2(w http.ResponseWriter, r *http.Request, _ auth.Device) {
	id := strings.TrimPrefix(r.URL.Query().Get("id"), "al-")
	a, err := h.Library.GetAlbum(r.Context(), id)
	if err != nil {
		writeErr(w, r, 70, "album not found")
		return
	}
	info := &albumInfo{MusicBrainzID: a.MBReleaseID}
	if h.AlbumImage != nil {
		notes, img := h.AlbumImage(r.Context(), a.ID)
		info.Notes = notes
		if img != "" {
			info.SmallImageURL, info.MediumImageURL, info.LargeImageURL = absURL(r, img, 120), absURL(r, img, 300), absURL(r, img, 1200)
		}
	}
	writeOK(w, r, h.Version, func(e *envelope) { e.AlbumInfo = info })
}
