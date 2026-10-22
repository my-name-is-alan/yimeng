package gateway

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
	"github.com/my-name-is-alan/yimeng/core/internal/lyrics/providers"
	"github.com/my-name-is-alan/yimeng/core/internal/metadata"
	"github.com/my-name-is-alan/yimeng/core/internal/webadmin"
)

// UserEdits is implemented by library.Scanner (bound lyrics, uploaded covers).
type UserEdits interface {
	SetUserLyrics(ctx context.Context, u library.UserLyrics) (library.UserLyrics, error)
	GetUserLyrics(ctx context.Context, trackID string) (library.UserLyrics, error)
	ClearUserLyrics(ctx context.Context, trackID string) error
	SetAlbumCover(ctx context.Context, albumID string, img []byte) error
	ClearAlbumCover(ctx context.Context, albumID string) error
}

// LyricsSearcher lists provider candidates for manual binding (LRCLIB).
type LyricsSearcher func(ctx context.Context, q, title, artist string) ([]providers.Candidate, error)

func (d Deps) mountWebUI(mux *http.ServeMux) {
	ui, _ := fs.Sub(webadmin.FS, "ui")
	mux.HandleFunc("GET /admin/{path...}", func(w http.ResponseWriter, r *http.Request) {
		p := path.Clean("/" + r.PathValue("path"))[1:]
		b, err := fs.ReadFile(ui, p)
		if p == "" || err != nil {
			// SPA: unknown paths fall back to the shell (routing is hash-based, but be lenient).
			if strings.Contains(path.Base(p), ".") && p != "" {
				http.NotFound(w, r)
				return
			}
			p = "index.html"
			if b, err = fs.ReadFile(ui, p); err != nil {
				http.NotFound(w, r)
				return
			}
		}
		ct := mime.TypeByExtension(path.Ext(p))
		if ct == "" {
			ct = "application/octet-stream"
		}
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob: https:; media-src 'self' blob:; style-src 'self' 'unsafe-inline'; connect-src 'self' ws: wss:; frame-ancestors 'none'")
		w.Write(b)
	})
	mux.HandleFunc("GET /admin", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/admin/", http.StatusFound) })
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/admin/", http.StatusFound) })
}

func (d Deps) mountWebAdminAPI(mux *http.ServeMux) {
	if d.UserEdits != nil {
		mux.HandleFunc("GET /api/tracks/{id}/lyrics/user", d.adminOnly(d.getUserLyrics))
		mux.HandleFunc("PUT /api/tracks/{id}/lyrics", d.adminOnly(d.localOnly("track", d.putUserLyrics)))
		mux.HandleFunc("DELETE /api/tracks/{id}/lyrics", d.adminOnly(d.localOnly("track", d.deleteUserLyrics)))
		mux.HandleFunc("PUT /api/albums/{id}/cover", d.adminOnly(d.localOnly("album", d.putAlbumCover)))
		mux.HandleFunc("POST /api/albums/{id}/cover/from-match", d.adminOnly(d.localOnly("album", d.albumCoverFromMatch)))
		mux.HandleFunc("DELETE /api/albums/{id}/cover", d.adminOnly(d.localOnly("album", d.deleteAlbumCover)))
	}
	mux.HandleFunc("GET /api/tracks/{id}/lyrics/search", d.adminOnly(d.localOnly("track", d.searchLyrics)))
	mux.HandleFunc("GET /api/tracks/{id}/match", d.adminOnly(d.localOnly("track", d.matchTrack)))
	mux.HandleFunc("GET /api/albums/{id}/match", d.adminOnly(d.localOnly("album", d.matchAlbum)))
	if d.Admin != nil {
		mux.HandleFunc("POST /api/overrides/batch", d.adminOnly(d.batchOverride))
	}
	mux.HandleFunc("GET /api/system/logs", d.adminOnly(d.getLogs))
	mux.HandleFunc("GET /api/settings/runtime", d.adminOnly(d.getRuntime))
}

func (d Deps) editErr(w http.ResponseWriter, what string, err error) {
	switch {
	case errors.Is(err, library.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not_found", "not found")
	case errors.Is(err, library.ErrBadInput):
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
	default:
		d.internal(w, what, err)
	}
}

func (d Deps) getUserLyrics(w http.ResponseWriter, r *http.Request) {
	u, err := d.UserEdits.GetUserLyrics(r.Context(), r.PathValue("id"))
	if err != nil {
		d.editErr(w, "user lyrics", err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (d Deps) putUserLyrics(w http.ResponseWriter, r *http.Request) {
	var b struct {
		LRC      string `json:"lrc"`
		OffsetMs int64  `json:"offset_ms"`
		Source   string `json:"source"`
	}
	if err := decodeBodyMax(r, &b, library.MaxUserLyricsBytes+4096); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if _, err := d.UserEdits.SetUserLyrics(r.Context(), library.UserLyrics{TrackID: r.PathValue("id"), LRC: b.LRC, OffsetMs: b.OffsetMs, Source: b.Source}); err != nil {
		d.editErr(w, "bind lyrics", err)
		return
	}
	d.Hub.Publish("library.lyrics", map[string]string{"track_id": r.PathValue("id")})
	d.trackLyrics(w, r)
}

func (d Deps) deleteUserLyrics(w http.ResponseWriter, r *http.Request) {
	if err := d.UserEdits.ClearUserLyrics(r.Context(), r.PathValue("id")); err != nil {
		d.editErr(w, "unbind lyrics", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) searchLyrics(w http.ResponseWriter, r *http.Request) {
	if d.LyricsSearch == nil {
		writeErr(w, http.StatusServiceUnavailable, "unavailable", "no lyrics search provider")
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	title, artist := "", ""
	if q == "" {
		t, err := d.Library.GetTrack(r.Context(), r.PathValue("id"))
		if err != nil {
			d.editErr(w, "track", err)
			return
		}
		title, artist = t.Title, t.ArtistName
	}
	if len(q) > 200 {
		writeErr(w, http.StatusBadRequest, "invalid_query", "q too long")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	items, err := d.LyricsSearch(ctx, q, title, artist)
	if err != nil {
		d.Log.Warn("lyrics search", "err", err)
		writeErr(w, http.StatusBadGateway, "provider_error", "lyrics provider unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (d Deps) putAlbumCover(w http.ResponseWriter, r *http.Request) {
	b, err := io.ReadAll(io.LimitReader(r.Body, library.MaxCoverBytes+1))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if err := d.UserEdits.SetAlbumCover(r.Context(), r.PathValue("id"), b); err != nil {
		d.editErr(w, "cover", err)
		return
	}
	d.Hub.Publish("library.cover", map[string]string{"album_id": r.PathValue("id")})
	al, err := d.Library.GetAlbum(r.Context(), r.PathValue("id"))
	if err != nil {
		d.editErr(w, "album", err)
		return
	}
	writeJSON(w, http.StatusOK, al)
}

// albumCoverFromMatch replaces an album's cover with a match candidate's
// image. The provider fetches it (its own host allowlist applies), and it is
// stored exactly like an uploaded cover: a local edit in the cache dir.
func (d Deps) albumCoverFromMatch(w http.ResponseWriter, r *http.Request) {
	if !d.matcher(w) {
		return
	}
	var b struct {
		Provider string `json:"provider"`
		URL      string `json:"url"`
	}
	if err := decodeBodyMax(r, &b, 4096); err != nil || b.Provider == "" || b.URL == "" {
		writeErr(w, http.StatusBadRequest, "invalid_body", "provider and url are required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	rc, err := d.Matcher.FetchCover(ctx, metadata.CoverCandidate{Provider: metadata.ProviderID(b.Provider), URL: b.URL})
	if err != nil {
		writeErr(w, http.StatusBadGateway, "provider_error", "cover fetch failed: "+err.Error())
		return
	}
	defer rc.Close()
	img, err := io.ReadAll(io.LimitReader(rc, library.MaxCoverBytes+1))
	if err != nil {
		writeErr(w, http.StatusBadGateway, "provider_error", "cover fetch failed")
		return
	}
	if err := d.UserEdits.SetAlbumCover(r.Context(), r.PathValue("id"), img); err != nil {
		d.editErr(w, "cover", err)
		return
	}
	d.Hub.Publish("library.cover", map[string]string{"album_id": r.PathValue("id")})
	al, err := d.Library.GetAlbum(r.Context(), r.PathValue("id"))
	if err != nil {
		d.editErr(w, "album", err)
		return
	}
	writeJSON(w, http.StatusOK, al)
}

func (d Deps) deleteAlbumCover(w http.ResponseWriter, r *http.Request) {
	if err := d.UserEdits.ClearAlbumCover(r.Context(), r.PathValue("id")); err != nil {
		d.editErr(w, "cover", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// MatchCandidate is a provider suggestion with confidence (0..1) for the metadata editor.
type MatchCandidate struct {
	Provider      string   `json:"provider"`
	ExternalID    string   `json:"external_id"`
	Score         float64  `json:"score"`
	Title         string   `json:"title"`
	Artist        string   `json:"artist,omitempty"`
	Album         string   `json:"album,omitempty"`
	AlbumArtist   string   `json:"album_artist,omitempty"`
	Genre         string   `json:"genre,omitempty"`
	TrackNo       int      `json:"track_no,omitempty"`
	DiscNo        int      `json:"disc_no,omitempty"`
	Year          int      `json:"year,omitempty"`
	DurationMs    int64    `json:"duration_ms,omitempty"`
	ISRC          string   `json:"isrc,omitempty"`
	MBRecordingID string   `json:"mb_recording_id,omitempty"`
	MBReleaseID   string   `json:"mb_release_id,omitempty"`
	Label         string   `json:"label,omitempty"`
	ReleaseDate   string   `json:"release_date,omitempty"`
	UPC           string   `json:"upc,omitempty"`
	CoverURL      string   `json:"cover_url,omitempty"`
	Popularity    int      `json:"popularity,omitempty"`
	Genres        []string `json:"genres,omitempty"`
	TrackCount    int      `json:"track_count,omitempty"`
}

func (d Deps) matcher(w http.ResponseWriter) bool {
	if d.Matcher == nil {
		writeErr(w, http.StatusServiceUnavailable, "unavailable", "metadata lookup not configured")
		return false
	}
	return true
}

func (d Deps) matchTrack(w http.ResponseWriter, r *http.Request) {
	if !d.matcher(w) {
		return
	}
	t, err := d.Library.GetTrack(r.Context(), r.PathValue("id"))
	if err != nil {
		d.editErr(w, "track", err)
		return
	}
	q := metadata.TrackQuery{Title: t.Title, Artist: t.ArtistName, Album: t.AlbumTitle, DurationMs: t.DurationMs}
	if v := strings.TrimSpace(r.URL.Query().Get("title")); v != "" {
		q.Title = v
	}
	if v := strings.TrimSpace(r.URL.Query().Get("artist")); v != "" {
		q.Artist = v
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	ms, err := d.Matcher.LookupTrack(ctx, q)
	if err != nil {
		d.Log.Warn("metadata match", "err", err)
		writeErr(w, http.StatusBadGateway, "provider_error", "metadata providers unavailable: "+err.Error())
		return
	}
	out := []MatchCandidate{}
	for _, m := range ms {
		out = append(out, MatchCandidate{Provider: string(m.Provider), ExternalID: m.ExternalID, Score: m.Score,
			Title: m.Title, Artist: m.Artist, Album: m.Album, AlbumArtist: m.AlbumArtist, Genre: m.Genre,
			TrackNo: m.TrackNo, DiscNo: m.DiscNo, Year: m.Year, DurationMs: m.DurationMs, ISRC: m.ISRC,
			MBRecordingID: m.MBRecordingID, MBReleaseID: m.MBReleaseID, ReleaseDate: m.ReleaseDate,
			CoverURL: m.CoverURL, Popularity: m.Popularity})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (d Deps) matchAlbum(w http.ResponseWriter, r *http.Request) {
	if !d.matcher(w) {
		return
	}
	al, err := d.Library.GetAlbum(r.Context(), r.PathValue("id"))
	if err != nil {
		d.editErr(w, "album", err)
		return
	}
	q := metadata.AlbumQuery{Title: al.Title, AlbumArtist: al.ArtistName, Year: al.Year}
	if v := strings.TrimSpace(r.URL.Query().Get("title")); v != "" {
		q.Title = v
	}
	if v := strings.TrimSpace(r.URL.Query().Get("artist")); v != "" {
		q.AlbumArtist = v
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	infos, err := d.Matcher.LookupAlbum(ctx, q)
	if err != nil {
		d.Log.Warn("metadata album match", "err", err)
		writeErr(w, http.StatusBadGateway, "provider_error", "metadata providers unavailable: "+err.Error())
		return
	}
	out := []MatchCandidate{}
	for _, in := range infos {
		c := MatchCandidate{Provider: string(in.Provider), ExternalID: in.ExternalID, Title: in.Title,
			AlbumArtist: in.AlbumArtist, Artist: in.AlbumArtist, Year: in.Year, MBReleaseID: in.MBReleaseID,
			Label: in.Label, ReleaseDate: in.ReleaseDate, UPC: in.UPC, CoverURL: in.CoverURL,
			Popularity: in.Popularity, Genres: in.Genres, TrackCount: in.TrackCount}
		if len(in.Genres) > 0 {
			c.Genre = in.Genres[0]
		}
		c.Score = albumScore(al, in)
		out = append(out, c)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// albumScore is a simple confidence: title match 0.6, artist 0.3, year 0.1.
func albumScore(al library.Album, in metadata.AlbumInfo) float64 {
	norm := func(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
	s := 0.0
	switch {
	case norm(al.Title) == norm(in.Title):
		s += 0.6
	case strings.Contains(norm(in.Title), norm(al.Title)) || strings.Contains(norm(al.Title), norm(in.Title)):
		s += 0.35
	}
	if norm(al.ArtistName) != "" && norm(al.ArtistName) == norm(in.AlbumArtist) {
		s += 0.3
	}
	if al.Year != 0 && al.Year == in.Year {
		s += 0.1
	}
	return s
}

func (d Deps) batchOverride(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Type  string         `json:"type"`
		IDs   []string       `json:"ids"`
		Patch map[string]any `json:"patch"`
	}
	if err := decodeBodyMax(r, &b, 1<<20); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if len(b.IDs) == 0 || len(b.IDs) > 1000 || len(b.Patch) == 0 {
		writeErr(w, http.StatusBadRequest, "invalid_request", "ids (1..1000) and patch are required")
		return
	}
	type res struct {
		ID     string `json:"id"`
		OK     bool   `json:"ok"`
		Error  string `json:"error,omitempty"`
		Origin string `json:"origin,omitempty"`
	}
	_, foreign, err := d.nonLocal(r, b.Type, b.IDs)
	if err != nil {
		if errors.Is(err, library.ErrBadInput) {
			writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
		} else {
			d.internal(w, "origin lookup", err)
		}
		return
	}
	out := []res{}
	skipped := []string{}
	okN := 0
	for _, id := range b.IDs {
		if o, bad := foreign[id]; bad {
			skipped = append(skipped, id)
			out = append(out, res{ID: id, Error: codeNotLocal, Origin: o})
			continue
		}
		if err := d.Admin.SetOverride(r.Context(), b.Type, id, b.Patch); err != nil {
			if errors.Is(err, library.ErrBadInput) {
				writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
			msg := "not found"
			if !errors.Is(err, library.ErrNotFound) {
				d.Log.Error("batch override", "id", id, "err", err)
				msg = "failed"
			}
			out = append(out, res{ID: id, Error: msg})
			continue
		}
		okN++
		out = append(out, res{ID: id, OK: true})
	}
	writeJSON(w, http.StatusOK, map[string]any{"updated": okN, "skipped_not_local": skipped, "results": out})
}

func (d Deps) getLogs(w http.ResponseWriter, r *http.Request) {
	if d.Logs == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		return
	}
	q := r.URL.Query()
	after, _ := strconv.ParseUint(q.Get("after"), 10, 64)
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var lv slog.Level
	if v := q.Get("level"); v != "" {
		if err := lv.UnmarshalText([]byte(v)); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_query", "level must be debug|info|warn|error")
			return
		}
	} else {
		lv = slog.LevelDebug
	}
	items := d.Logs.Since(after, lv, limit)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (d Deps) getRuntime(w http.ResponseWriter, r *http.Request) {
	rt := d.Runtime
	if rt == nil {
		rt = map[string]any{}
	}
	writeJSON(w, http.StatusOK, rt)
}
