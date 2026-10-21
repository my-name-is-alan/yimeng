package gateway

import (
	"errors"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
	Album         library.Album   `json:"album"`
	Tracks        []library.Track `json:"tracks"`
	Credits       []any           `json:"credits"` // filled by Metadata (P1)
	RelatedAlbums []library.Album `json:"related_albums"`
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
		page := AlbumPage{Album: al, Tracks: tr.Items, Credits: []any{}, RelatedAlbums: []library.Album{}}
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
	d.serveFile(w, r, path, err, "", "private, max-age=86400")
}

func (d Deps) trackCover(w http.ResponseWriter, r *http.Request) {
	path, err := d.Library.TrackCover(r.Context(), r.PathValue("id"))
	d.serveFile(w, r, path, err, "", "private, max-age=86400")
}

var audioTypes = map[string]string{
	"mp3": "audio/mpeg", "flac": "audio/flac", "m4a": "audio/mp4", "ogg": "audio/ogg",
	"opus": "audio/ogg", "wav": "audio/wav", "dsf": "audio/x-dsf",
}

// stream serves the local file for a canonical track with HTTP Range support.
// The path comes only from the DB and is re-validated against library roots.
func (d Deps) stream(w http.ResponseWriter, r *http.Request) {
	f, err := d.Library.TrackFile(r.Context(), r.PathValue("id"))
	ct := audioTypes[f.Format]
	if ct == "" {
		ct = "application/octet-stream"
	}
	d.serveFile(w, r, f.Path, err, ct, "private, no-transform")
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
