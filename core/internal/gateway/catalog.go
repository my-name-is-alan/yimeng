package gateway

import (
	"errors"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/my-name-is-alan/yimeng/core/internal/covers"
	"github.com/my-name-is-alan/yimeng/core/internal/library"
)

func listParams(r *http.Request) (library.ListParams, error) {
	q := r.URL.Query()
	var p library.ListParams
	var err error
	if v := q.Get("limit"); v != "" {
		if p.Limit, err = strconv.Atoi(v); err != nil || p.Limit < 1 {
			return p, errors.New("limit must be a positive integer")
		}
	}
	if v := q.Get("offset"); v != "" {
		if p.Offset, err = strconv.Atoi(v); err != nil || p.Offset < 0 {
			return p, errors.New("offset must be a non-negative integer")
		}
	}
	switch strings.ToLower(q.Get("order")) {
	case "", "asc":
	case "desc":
		p.Desc = true
	default:
		return p, errors.New("order must be asc or desc")
	}
	p.Sort = q.Get("sort")
	p.Q = strings.TrimSpace(q.Get("q"))
	if len(p.Q) > 200 {
		p.Q = p.Q[:200]
	}
	p.ArtistID = q.Get("artist_id")
	p.AlbumID = q.Get("album_id")
	if v := q.Get("favorite"); v != "" {
		if p.Favorite, err = strconv.ParseBool(v); err != nil {
			return p, errors.New("favorite must be true or false")
		}
	}
	return p, nil
}

func (d Deps) list(w http.ResponseWriter, r *http.Request, what string, f func(library.ListParams) (any, error)) {
	p, err := listParams(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_query", err.Error())
		return
	}
	d.writeOne(w, what, func() (any, error) { return f(p) })
}

func (d Deps) listArtists(w http.ResponseWriter, r *http.Request) {
	d.list(w, r, "artists", func(p library.ListParams) (any, error) { return d.Library.ListArtists(r.Context(), p) })
}

func (d Deps) listAlbums(w http.ResponseWriter, r *http.Request) {
	d.list(w, r, "albums", func(p library.ListParams) (any, error) { return d.Library.ListAlbums(r.Context(), p) })
}

func (d Deps) listTracks(w http.ResponseWriter, r *http.Request) {
	d.list(w, r, "tracks", func(p library.ListParams) (any, error) { return d.Library.ListTracks(r.Context(), p) })
}

func (d Deps) getTrack(w http.ResponseWriter, r *http.Request) {
	d.writeOne(w, "track", func() (any, error) { return d.Library.GetTrack(r.Context(), r.PathValue("id")) })
}

type AlbumPage struct {
	Album         library.Album    `json:"album"`
	Tracks        []library.Track  `json:"tracks"`
	Credits       []library.Credit `json:"credits"` // from metadata providers (opt-in)
	RelatedAlbums []library.Album  `json:"related_albums"`
}

func (d Deps) getAlbum(w http.ResponseWriter, r *http.Request) {
	d.writeOne(w, "album", func() (any, error) {
		ctx := r.Context()
		al, err := d.Library.GetAlbum(ctx, r.PathValue("id"))
		if err != nil {
			return nil, err
		}
		tr, err := d.Library.ListTracks(ctx, library.ListParams{AlbumID: al.ID, Limit: 500})
		if err != nil {
			return nil, err
		}
		cr, err := d.Library.AlbumCredits(ctx, al.ID)
		if err != nil {
			return nil, err
		}
		page := AlbumPage{Album: al, Tracks: tr.Items, Credits: cr, RelatedAlbums: []library.Album{}}
		if len(al.Artists) > 0 {
			rel, err := d.Library.ListAlbums(ctx, library.ListParams{ArtistID: al.Artists[0].ID, Sort: "year", Limit: 20})
			if err != nil {
				return nil, err
			}
			for _, a := range rel.Items {
				if a.ID != al.ID {
					page.RelatedAlbums = append(page.RelatedAlbums, a)
				}
			}
		}
		return page, nil
	})
}

type ArtistPage struct {
	Artist library.Artist  `json:"artist"`
	Albums []library.Album `json:"albums"`
	Tracks []library.Track `json:"tracks"`
}

func (d Deps) getArtist(w http.ResponseWriter, r *http.Request) {
	d.writeOne(w, "artist", func() (any, error) {
		ctx := r.Context()
		a, err := d.Library.GetArtist(ctx, r.PathValue("id"))
		if err != nil {
			return nil, err
		}
		al, err := d.Library.ListAlbums(ctx, library.ListParams{ArtistID: a.ID, Sort: "year", Limit: 200})
		if err != nil {
			return nil, err
		}
		tr, err := d.Library.ListTracks(ctx, library.ListParams{ArtistID: a.ID, Sort: "album", Limit: 20})
		if err != nil {
			return nil, err
		}
		return ArtistPage{Artist: a, Albums: al.Items, Tracks: tr.Items}, nil
	})
}

type SearchResult struct {
	Artists []library.Artist `json:"artists"`
	Albums  []library.Album  `json:"albums"`
	Tracks  []library.Track  `json:"tracks"`
}

func (d Deps) search(w http.ResponseWriter, r *http.Request) {
	p, err := listParams(r)
	if err != nil || p.Q == "" {
		writeErr(w, http.StatusBadRequest, "invalid_query", "q is required")
		return
	}
	if p.Limit == 0 || p.Limit > 50 {
		p.Limit = 20
	}
	p.Offset, p.Sort, p.Desc, p.ArtistID, p.AlbumID = 0, "", false, "", ""
	d.writeOne(w, "search", func() (any, error) {
		ctx := r.Context()
		ar, err := d.Library.ListArtists(ctx, p)
		if err != nil {
			return nil, err
		}
		al, err := d.Library.ListAlbums(ctx, p)
		if err != nil {
			return nil, err
		}
		tr, err := d.Library.ListTracks(ctx, p)
		if err != nil {
			return nil, err
		}
		return SearchResult{Artists: ar.Items, Albums: al.Items, Tracks: tr.Items}, nil
	})
}

func (d Deps) albumCover(w http.ResponseWriter, r *http.Request) {
	path, err := d.Library.AlbumCover(r.Context(), r.PathValue("id"))
	d.serveCover(w, r, path, err)
}

func (d Deps) trackCover(w http.ResponseWriter, r *http.Request) {
	path, err := d.Library.TrackCover(r.Context(), r.PathValue("id"))
	d.serveCover(w, r, path, err)
}

func (d Deps) serveCover(w http.ResponseWriter, r *http.Request, path string, err error) {
	if err != nil || path == "" || d.Covers == nil {
		d.serveFile(w, r, path, err, "", "private, max-age=86400")
		return
	}
	q := r.URL.Query()
	if q.Get("size") == "" && q.Get("format") == "" {
		d.serveFile(w, r, path, nil, "", "private, max-age=86400")
		return
	}
	size, err := covers.ParseSize(q.Get("size"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_query", err.Error())
		return
	}
	format := q.Get("format")
	thumb, ct, err := d.Covers.Thumb(path, size, format)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_query", err.Error())
		return
	}
	d.serveFile(w, r, thumb, nil, ct, "private, max-age=604800")
}

var audioTypes = map[string]string{
	"mp3": "audio/mpeg", "flac": "audio/flac", "m4a": "audio/mp4", "ogg": "audio/ogg",
	"opus": "audio/ogg", "wav": "audio/wav", "dsf": "audio/x-dsf",
}

func (d Deps) serveFile(w http.ResponseWriter, r *http.Request, path string, err error, contentType, cache string) {
	if errors.Is(err, library.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not_found", "file not found")
		return
	}
	if err != nil {
		d.internal(w, "resolve file", err)
		return
	}
	fh, err := os.Open(path)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not_found", "file not available")
		return
	}
	defer fh.Close()
	st, err := fh.Stat()
	if err != nil || !st.Mode().IsRegular() {
		writeErr(w, http.StatusNotFound, "not_found", "file not available")
		return
	}
	if contentType == "" {
		contentType = mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	}
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Header().Set("Cache-Control", cache)
	http.ServeContent(w, r, "", st.ModTime(), fh)
}

func (d Deps) trackCredits(w http.ResponseWriter, r *http.Request) {
	d.writeOne(w, "track", func() (any, error) {
		if _, err := d.Library.GetTrack(r.Context(), r.PathValue("id")); err != nil {
			return nil, err
		}
		cr, err := d.Library.TrackCredits(r.Context(), r.PathValue("id"))
		return map[string]any{"items": cr}, err
	})
}

func (d Deps) enrich(w http.ResponseWriter, r *http.Request) {
	if d.Enrich == nil {
		writeErr(w, http.StatusServiceUnavailable, "source_unavailable", "no metadata provider enabled (set metadata.musicbrainz.enabled)")
		return
	}
	d.Enrich()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "scheduled"})
}

func (d Deps) trackLyrics(w http.ResponseWriter, r *http.Request) {
	l, err := d.Library.Lyrics(r.Context(), r.PathValue("id"), d.LyricsFetcher)
	switch {
	case errors.Is(err, library.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not_found", "track not found")
	case errors.Is(err, library.ErrNoLyrics):
		writeErr(w, http.StatusNotFound, "no_lyrics", "no lyrics for this track")
	case err != nil:
		d.internal(w, "lyrics", err)
	default:
		writeJSON(w, http.StatusOK, l)
	}
}

func (d Deps) listProviders(w http.ResponseWriter, r *http.Request) {
	items := []map[string]any{}
	if d.Providers != nil {
		for _, p := range d.Providers.All() {
			items = append(items, map[string]any{
				"id": p.ID(), "capabilities": uint32(p.Capabilities()), "health": p.Health(r.Context()),
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
