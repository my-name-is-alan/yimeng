package gateway

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/covers"
	"github.com/my-name-is-alan/yimeng/core/internal/library"
	"github.com/my-name-is-alan/yimeng/core/internal/online"
)

// Cover API (D45):
//
//	GET /api/v1/covers/{id}?size=120|300|600|1200&format=webp|jpeg
//	GET /api/v1/covers/{id}/meta → {url, blurhash, colors[3]}
//
// {id} is an album or track id (local or online). Online covers are
// downloaded once and kept on disk; every variant is immutable with a strong
// ETag. Auth: Authorization header, ?access_token=, or a signed URL.

var coverSizes = map[int]bool{120: true, 300: true, 600: true, 1200: true}

func (d Deps) resolveCover(ctx context.Context, id string) (string, error) {
	p, err := d.Library.AlbumCover(ctx, id)
	if err != nil {
		p, err = d.Library.TrackCover(ctx, id)
	}
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
		if d.Covers == nil {
			return "", library.ErrNotFound
		}
		lp, err := d.Covers.Remote(ctx, p)
		if err != nil {
			return "", library.ErrNotFound
		}
		return lp, nil
	}
	return p, nil
}

func (d Deps) coverV1(w http.ResponseWriter, r *http.Request) {
	src, err := d.resolveCover(r.Context(), r.PathValue("id"))
	if err != nil || d.Covers == nil {
		writeErr(w, http.StatusNotFound, "not_found", "no cover")
		return
	}
	q := r.URL.Query()
	size := 600
	if v := q.Get("size"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || !coverSizes[n] {
			writeErr(w, http.StatusBadRequest, "invalid_query", "size must be 120, 300, 600 or 1200")
			return
		}
		size = n
	}
	format := strings.ToLower(q.Get("format"))
	switch format {
	case "":
		format = "jpeg"
		if strings.Contains(r.Header.Get("Accept"), "image/webp") {
			format = "webp"
		}
	case "jpg", "jpeg", "webp":
	default:
		writeErr(w, http.StatusBadRequest, "invalid_query", "format must be webp or jpeg")
		return
	}
	thumb, ct, err := d.Covers.Thumb(src, size, format)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not_found", "cover not decodable")
		return
	}
	et := covers.ETag(thumb)
	w.Header().Set("ETag", et)
	w.Header().Set("Vary", "Accept")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	if et != "" && r.Header.Get("If-None-Match") == et {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	d.serveFile(w, r, thumb, nil, ct, "private, max-age=31536000, immutable")
}

func (d Deps) coverMeta(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	src, err := d.resolveCover(r.Context(), id)
	if err != nil && d.Online != nil && d.Covers != nil && library.IsExternalID(id) {
		// online track: meta of the platform cover; url stays the platform CDN URL
		if t, terr := d.Online.Track(r.Context(), id); terr == nil && t.CoverURL != "" {
			if lp, rerr := d.Covers.Remote(r.Context(), t.CoverURL); rerr == nil {
				if m, merr := d.Covers.Meta(lp); merr == nil {
					w.Header().Set("Cache-Control", "private, max-age=86400")
					writeJSON(w, http.StatusOK, library.CoverRef{URL: strings.Replace(t.CoverURL, "http://", "https://", 1), Blurhash: m.Blurhash, Colors: m.Colors})
					return
				}
			}
		}
	}
	if err != nil || d.Covers == nil {
		writeErr(w, http.StatusNotFound, "not_found", "no cover")
		return
	}
	m, err := d.Covers.Meta(src)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not_found", "cover not decodable")
		return
	}
	dev, _ := DeviceFrom(r.Context())
	w.Header().Set("Cache-Control", "private, max-age=86400")
	writeJSON(w, http.StatusOK, library.CoverRef{URL: d.signedCover(dev.ID, id), Blurhash: m.Blurhash, Colors: m.Colors})
}

// signedCover returns a ready cover URL (600px) for a device.
func (d Deps) signedCover(devID, id string) string {
	p := "/api/v1/covers/" + url.PathEscape(id)
	if d.Auth == nil {
		return p
	}
	return d.Auth.SignURL(p, devID, 0)
}

func (d Deps) signedStream(devID, id string) string {
	p := "/api/v1/stream/" + url.PathEscape(id)
	if d.Auth == nil {
		return p
	}
	return d.Auth.SignURL(p, devID, 0)
}

// cover meta is computed off the request path, a few at a time.
var (
	metaOnce  sync.Once
	metaQueue chan string
	metaSeen  sync.Map
)

func (d Deps) queueMeta(src string) {
	if d.Covers == nil || src == "" {
		return
	}
	metaOnce.Do(func() {
		metaQueue = make(chan string, 512)
		c := d.Covers
		for i := 0; i < 2; i++ {
			go func() {
				for p := range metaQueue {
					c.Meta(p)
					metaSeen.Delete(p)
				}
			}()
		}
	})
	if _, dup := metaSeen.LoadOrStore(src, true); dup {
		return
	}
	select {
	case metaQueue <- src:
	default:
		metaSeen.Delete(src)
	}
}

// coverRef builds the cover object for an entity with a cover source.
func (d Deps) coverRef(devID, id, src string) *library.CoverRef {
	if src == "" {
		return nil
	}
	ref := &library.CoverRef{URL: d.signedCover(devID, id)}
	if d.Covers == nil {
		return ref
	}
	local := src
	if strings.HasPrefix(src, "http") {
		local = "" // meta after the first download (cover endpoint / prefetch)
	}
	if local != "" {
		if m, ok := d.Covers.MetaCached(local); ok {
			ref.Blurhash, ref.Colors = m.Blurhash, m.Colors
		} else {
			d.queueMeta(local)
		}
	} else if lp, ok := d.Covers.RemoteCached(src); ok {
		if m, ok := d.Covers.MetaCached(lp); ok {
			ref.Blurhash, ref.Colors = m.Blurhash, m.Colors
		} else {
			d.queueMeta(lp)
		}
	}
	return ref
}

// decorateTracks adds signed cover/stream URLs and placeholder data.
func (d Deps) decorateTracks(ctx context.Context, ts []library.Track) {
	dev, _ := DeviceFrom(ctx)
	d.markEntitlement(ctx, ts)
	for i := range ts {
		t := &ts[i]
		if t.AlbumID != "" && t.CoverPath != "" {
			t.Cover = d.coverRef(dev.ID, t.AlbumID, t.CoverPath)
		}
		t.StreamURL = d.signedStream(dev.ID, t.ID)
	}
}

// Online covers: the platform CDN URL is used as-is; once Core has a disk
// copy (fetched in the background, paced) the placeholder data is added.
var (
	remoteOnce  sync.Once
	remoteQueue chan string
	remoteSeen  sync.Map
)

func (d Deps) onlineCover(u string) *online.CoverRef {
	if u == "" {
		return nil
	}
	ref := &online.CoverRef{URL: strings.Replace(u, "http://", "https://", 1)}
	if d.Covers == nil {
		return ref
	}
	if lp, ok := d.Covers.RemoteCached(u); ok {
		if m, ok := d.Covers.MetaCached(lp); ok {
			ref.Blurhash, ref.Colors = m.Blurhash, m.Colors
		} else {
			d.queueMeta(lp)
		}
		return ref
	}
	remoteOnce.Do(func() {
		remoteQueue = make(chan string, 256)
		c := d.Covers
		go func() {
			for u := range remoteQueue {
				if lp, err := c.Remote(context.Background(), u); err == nil {
					c.Meta(lp)
				}
				remoteSeen.Delete(u)
				time.Sleep(150 * time.Millisecond) // be gentle with CDNs
			}
		}()
	})
	if _, dup := remoteSeen.LoadOrStore(u, true); !dup {
		select {
		case remoteQueue <- u:
		default:
			remoteSeen.Delete(u)
		}
	}
	return ref
}

func (d Deps) decorateOnlineTracks(ts []online.Track) {
	for i := range ts {
		ts[i].Cover = d.onlineCover(ts[i].CoverURL)
	}
}

func (d Deps) decorateOnlinePlaylists(ps []online.Playlist) {
	for i := range ps {
		ps[i].Cover = d.onlineCover(ps[i].CoverURL)
	}
}

// markEntitlement flags online snapshots that are preview-only or taken
// down upstream (D51); playback still auto-switches (D50).
func (d Deps) markEntitlement(ctx context.Context, ts []library.Track) {
	src, ok := d.OnlineLib.(interface {
		ExternalStatus(context.Context, []string) (map[string]string, error)
	})
	if !ok {
		return
	}
	var ids []string
	for _, t := range ts {
		if library.IsExternalID(t.ID) {
			ids = append(ids, t.ID)
		}
	}
	if len(ids) == 0 {
		return
	}
	st, err := src.ExternalStatus(ctx, ids)
	if err != nil {
		return
	}
	for i := range ts {
		if e, ok := st[ts[i].ID]; ok {
			ts[i].Entitlement = e
			ts[i].Playable = e != string(online.EntUnavailable)
			ts[i].Preview = e == string(online.EntPreview)
		}
	}
}
