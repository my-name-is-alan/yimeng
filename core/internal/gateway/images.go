package gateway

// Image cache endpoints (D60).
//
//	GET /api/v1/images/{key}?size=120|300|600|1200&format=webp|jpeg
//	    A remote album cover / artist image Core has seen in a platform or
//	    metadata response (key = hash of the URL). Public: the content is
//	    public CDN imagery and only registered, allow-listed URLs resolve,
//	    so this is not an open proxy.
//	GET /api/v1/artists/{id}/image  user image → online artist picture →
//	    auto-matched picture (metadata providers) → newest album cover.

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/covers"
	"github.com/my-name-is-alan/yimeng/core/internal/library"
	"github.com/my-name-is-alan/yimeng/core/internal/metadata"
	"github.com/my-name-is-alan/yimeng/core/internal/online"
	"github.com/my-name-is-alan/yimeng/core/internal/owner"
)

func (d Deps) imageByKey(w http.ResponseWriter, r *http.Request) {
	if d.Covers == nil {
		writeErr(w, http.StatusNotFound, "not_found", "image cache disabled")
		return
	}
	p, err := d.Covers.RemoteKey(r.Context(), r.PathValue("key"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "not_found", "image not available")
		return
	}
	d.serveSized(w, r, p)
}

// serveSized serves a local image file at ?size= (preset or 1..2048) / ?format=.
func (d Deps) serveSized(w http.ResponseWriter, r *http.Request, src string) {
	q := r.URL.Query()
	out, ct := src, ""
	if q.Get("size") != "" || q.Get("format") != "" {
		size, err := covers.ParseSize(q.Get("size"))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_query", err.Error())
			return
		}
		format := strings.ToLower(q.Get("format"))
		if format == "" && strings.Contains(r.Header.Get("Accept"), "image/webp") {
			format = "webp"
		}
		if format != "" && format != "jpeg" && format != "jpg" && format != "webp" && format != "png" {
			writeErr(w, http.StatusBadRequest, "invalid_query", "format must be webp, jpeg or png")
			return
		}
		out, ct, err = d.Covers.Thumb(src, size, format)
		if err != nil {
			writeErr(w, http.StatusNotFound, "not_found", "image not decodable")
			return
		}
		w.Header().Set("Vary", "Accept")
	} else {
		ct = sniffImage(src)
	}
	et := covers.ETag(out)
	w.Header().Set("ETag", et)
	if et != "" && r.Header.Get("If-None-Match") == et {
		w.Header().Set("Cache-Control", "public, max-age=604800")
		w.WriteHeader(http.StatusNotModified)
		return
	}
	d.serveFile(w, r, out, nil, ct, "public, max-age=604800")
}

func sniffImage(p string) string {
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	b := make([]byte, 512)
	n, _ := f.Read(b)
	ct := http.DetectContentType(b[:n])
	if strings.HasPrefix(ct, "image/") {
		return ct
	}
	return "application/octet-stream"
}

// imageURL returns the Core URL of a cached remote image ("" if the host is not allowed).
func (d Deps) imageURL(u string) string {
	if d.Covers == nil || u == "" {
		return ""
	}
	if k, ok := d.Covers.Register(u); ok {
		return "/api/v1/images/" + k
	}
	return ""
}

// remoteArtistImage returns the remote image URL for an artist id, or "".
// Online artists use the platform picture (remembered, so it survives
// platform risk control); local artists use an auto-matched picture.
func (d Deps) remoteArtistImage(ctx context.Context, id string) string {
	if d.Covers == nil {
		return ""
	}
	alias := "artist:" + id
	if library.IsExternalID(id) {
		// remembered picture first: no platform call on repeat hits; search /
		// artist responses keep the alias fresh (decorateOnlineArtists)
		if u, ok := d.Covers.Aliased(alias); ok {
			return u
		}
		if d.Online != nil {
			if a, err := d.Online.Artist(ctx, id); err == nil && a.PictureURL != "" {
				d.Covers.Alias(alias, a.PictureURL)
				return a.PictureURL
			}
		}
		u, _ := d.Covers.Aliased(alias)
		return u
	}
	if u, ok := d.Covers.Aliased(alias); ok {
		return u
	}
	d.queueArtistMatch(ctx, id)
	return ""
}

// ── automatic artist pictures for local artists ─────────────────────────

var (
	artistMatchOnce  sync.Once
	artistMatchQueue chan artistJob
	artistMatchSeen  sync.Map // id → time of last attempt
)

const artistMatchRetry = 7 * 24 * time.Hour

type artistJob struct {
	id  string
	own owner.Owner
}

func (d Deps) queueArtistMatch(ctx context.Context, id string) {
	if d.Matcher == nil || d.Library == nil || !d.ArtistImageAuto {
		return
	}
	if t, ok := artistMatchSeen.Load(id); ok && time.Since(t.(time.Time)) < artistMatchRetry {
		return
	}
	artistMatchSeen.Store(id, time.Now())
	artistMatchOnce.Do(func() {
		artistMatchQueue = make(chan artistJob, 512)
		go func() {
			for j := range artistMatchQueue {
				d.matchArtistImage(j.own, j.id)
				time.Sleep(1500 * time.Millisecond) // paced: be gentle with providers
			}
		}()
	})
	select {
	case artistMatchQueue <- artistJob{id, owner.From(ctx)}:
	default:
		artistMatchSeen.Delete(id)
	}
}

func normName(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

// matchArtistImage only accepts an exact (normalised) name match so a wrong
// person's photo is never attached automatically.
func (d Deps) matchArtistImage(own owner.Owner, id string) {
	ctx, cancel := context.WithTimeout(owner.With(context.Background(), own.ID, own.Admin), 20*time.Second)
	defer cancel()
	a, err := d.Library.GetArtist(ctx, id)
	if err != nil || a.Name == "" {
		d.Log.Info("artist image: artist not found", "artist", id)
		return
	}
	infos, err := d.Matcher.LookupArtist(ctx, metadata.ArtistQuery{Name: a.Name, MBArtistID: a.MBID})
	if err != nil {
		d.Log.Info("artist image lookup failed", "artist", id, "err", err.Error())
		artistMatchSeen.Store(id, time.Now().Add(-artistMatchRetry+time.Hour)) // retry in an hour
		return
	}
	defer func() {
		if _, ok := d.Covers.Aliased("artist:" + id); !ok {
			d.Log.Info("artist image: no exact-name match", "artist", id, "candidates", len(infos))
		}
	}()
	for _, in := range infos {
		if normName(in.Name) != normName(a.Name) {
			continue
		}
		for _, u := range in.ImageURLs {
			if d.Covers.HostAllowed(strings.Replace(u, "http://", "https://", 1)) {
				if _, err := d.Covers.Remote(ctx, u); err == nil {
					d.Covers.Alias("artist:"+id, u)
					d.Log.Info("artist image matched", "artist", id, "provider", in.Provider)
					return
				}
			}
		}
	}
}

// artistImage: GET /api/artists/{id}/image[?size=&format=]
func (d Deps) artistImage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if d.UserEdits != nil && !library.IsExternalID(id) {
		if p, err := d.UserEdits.ArtistImage(r.Context(), id); err == nil && p != "" {
			if d.Covers == nil {
				d.serveFile(w, r, p, nil, "", "private, max-age=86400")
				return
			}
			d.serveSized(w, r, p)
			return
		}
	}
	if u := d.remoteArtistImage(r.Context(), id); u != "" {
		if p, err := d.Covers.Remote(r.Context(), u); err == nil {
			d.serveSized(w, r, p)
			return
		}
	}
	// fallback: the artist's newest album cover
	if !library.IsExternalID(id) {
		if pg, err := d.Library.ListAlbums(r.Context(), library.ListParams{ArtistID: id, Limit: 1, Sort: "year", Desc: true}); err == nil && len(pg.Items) > 0 && pg.Items[0].HasCover {
			if p, err := d.Library.AlbumCover(r.Context(), pg.Items[0].ID); err == nil {
				d.serveCover(w, r, p, nil)
				return
			}
		}
	}
	writeErr(w, http.StatusNotFound, "not_found", "no artist image")
}

// cacheStatsAll merges the platform cache stats with the image cache stats.
func (d *Deps) cacheStatsAll(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{}
	if d.Online != nil && d.Online.Cache != nil {
		st, err := d.Online.Cache.Stats(r.Context())
		if err != nil {
			d.internal(w, "cache stats", err)
			return
		}
		b, _ := json.Marshal(st)
		json.Unmarshal(b, &out)
	}
	if d.Covers != nil {
		out["images"] = d.Covers.ImageStats()
	}
	writeJSON(w, http.StatusOK, out)
}

func (d *Deps) clearCacheAll(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("kind") == "images" {
		if d.Covers == nil {
			writeErr(w, http.StatusNotFound, "not_supported", "image cache disabled")
			return
		}
		n, err := d.Covers.ClearImages()
		if err != nil {
			d.internal(w, "clear images", err)
			return
		}
		d.Log.Info("image cache cleared", "files", n)
		writeJSON(w, http.StatusOK, map[string]any{"deleted": n})
		return
	}
	d.clearCache(w, r)
}

// artistAlias returns a remembered remote picture of an artist (no network);
// for local artists without one a background match is queued.
func (d Deps) artistAlias(ctx context.Context, id string) (string, bool) {
	if d.Covers == nil {
		return "", false
	}
	if u, ok := d.Covers.Aliased("artist:" + id); ok {
		return u, true
	}
	if !library.IsExternalID(id) {
		d.queueArtistMatch(ctx, id)
	}
	return "", false
}

func (d Deps) subsonicArtistInfo(ctx context.Context, id string) (bio, img string) {
	if library.IsExternalID(id) && d.Online != nil {
		if a, err := d.Online.Artist(ctx, id); err == nil {
			bio = a.Description
		}
	}
	return bio, d.imageURL(d.remoteArtistImage(ctx, id))
}

func (d Deps) subsonicAlbumImage(ctx context.Context, id string) (notes, img string) {
	if p, err := d.Library.AlbumCover(ctx, id); err == nil && (strings.HasPrefix(p, "https://") || strings.HasPrefix(p, "http://")) {
		img = d.imageURL(p)
	}
	return "", img
}

// decorateOnlineResult routes every online cover / artist picture through the
// image cache and remembers artist pictures for risk-control fallback.
func (d Deps) decorateOnlineResult(r *online.SearchResult) {
	d.decorateOnlineTracks(r.Tracks)
	d.decorateOnlinePlaylists(r.Playlists)
	d.decorateOnlineAlbums(r.Albums)
	d.decorateOnlineArtists(r.Artists)
}

func (d Deps) decorateOnlineAlbums(as []online.Album) {
	for i := range as {
		as[i].Cover = d.onlineCover(as[i].CoverURL)
	}
}

func (d Deps) decorateOnlineArtists(as []online.Artist) {
	for i := range as {
		if as[i].PictureURL == "" {
			continue
		}
		if d.Covers != nil && as[i].ID != "" {
			d.Covers.Alias("artist:"+as[i].ID, as[i].PictureURL)
		}
		as[i].Cover = d.onlineCover(as[i].PictureURL)
	}
}

func (d Deps) decorateOnlinePlaylist(p *online.Playlist) {
	p.Cover = d.onlineCover(p.CoverURL)
	d.decorateOnlineTracks(p.Tracks)
}
