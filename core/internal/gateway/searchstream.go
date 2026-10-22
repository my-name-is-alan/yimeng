package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
)

// wantsSearchStream: GET /search?q=&stream=1 (or Accept: text/event-stream).
func wantsSearchStream(r *http.Request) bool {
	return r.URL.Query().Get("stream") == "1" || strings.Contains(r.Header.Get("Accept"), "text/event-stream")
}

// searchStream sends grouped results incrementally as SSE:
//
//	event: local   data: {artists, albums, tracks}           (immediately)
//	event: online  data: {origin, name, result|error}        (one per platform, as each finishes)
//	event: done    data: {}
func (d Deps) searchStream(w http.ResponseWriter, r *http.Request, p library.ListParams) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "internal", "streaming unsupported")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	send := func(ev string, v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev, b)
		fl.Flush()
	}
	ctx := r.Context()
	res := SearchResult{Artists: []library.Artist{}, Albums: []library.Album{}, Tracks: []library.Track{}}
	if ar, err := d.Library.ListArtists(ctx, p); err == nil {
		res.Artists = ar.Items
	}
	if al, err := d.Library.ListAlbums(ctx, p); err == nil {
		res.Albums = al.Items
	}
	if tr, err := d.Library.ListTracks(ctx, p); err == nil {
		res.Tracks = tr.Items
	}
	d.decorateArtists(ctx, res.Artists)
	d.decorateAlbums(ctx, res.Albums)
	d.decorateTracks(ctx, res.Tracks)
	w.WriteHeader(http.StatusOK)
	send("local", res)
	if d.Online != nil && r.URL.Query().Get("online") != "0" {
		origins := d.Online.Reg.EnabledIDs()
		if o := r.URL.Query().Get("origins"); o != "" {
			origins = strings.Split(o, ",")
		}
		ch := make(chan any, len(origins))
		for _, o := range origins {
			go func(o string) {
				rs := d.Online.Search(ctx, p.Q, p.Limit, []string{o})
				if len(rs) == 0 {
					ch <- map[string]any{"origin": o, "error": "not_enabled"}
					return
				}
				if rs[0].Result != nil {
					d.decorateOnlineTracks(rs[0].Result.Tracks)
					d.decorateOnlinePlaylists(rs[0].Result.Playlists)
				}
				ch <- rs[0]
			}(o)
		}
		for range origins {
			select {
			case v := <-ch:
				send("online", v)
			case <-ctx.Done():
				return
			}
		}
	}
	send("done", map[string]any{})
}
