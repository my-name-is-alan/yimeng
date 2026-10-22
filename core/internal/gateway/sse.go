package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
)

// GET /api/v1/events — Server-Sent Events with the same events as /ws
// (favorites.changed, playlists.changed, history.added, progress.changed,
// library.updated, player.*, …). Auth: Bearer header or ?access_token=/?token=.
// ?types=a,b filters. Keepalive comment every 15 s.
func (d Deps) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "internal", "streaming unsupported")
		return
	}
	dev, _ := DeviceFrom(r.Context())
	want := map[string]bool{}
	for _, t := range strings.Split(r.URL.Query().Get("types"), ",") {
		if t = strings.TrimSpace(t); t != "" {
			want[t] = true
		}
	}
	ch, unsub := d.Hub.Subscribe()
	defer unsub()
	may := d.playerFilter(r.Context(), dev)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "event: hello\ndata: {\"device_id\":%q}\n\n", dev.ID)
	fl.Flush()
	rc := http.NewResponseController(w)
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			fl.Flush()
		case e, ok := <-ch:
			if !ok {
				fmt.Fprint(w, "event: reconnect\ndata: {}\n\n")
				fl.Flush()
				return
			}
			if len(want) > 0 && !want[e.Type] {
				continue
			}
			data, ok := eventFor(dev, e.Data)
			if !ok {
				continue
			}
			if pid := eventPlayer(data); pid != "" && !may(pid) {
				continue // another account's player (D49)
			}
			b, err := json.Marshal(data)
			if err != nil {
				continue
			}
			rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, b); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

// GET /api/v1/search/suggest?q= — fast prefix/pinyin suggestions {items:[string]}.
func (d Deps) searchSuggest(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]any{"items": []string{}})
		return
	}
	if len(q) > 64 {
		q = q[:64]
	}
	seen := map[string]bool{}
	items := []string{}
	add := func(s string) {
		if s != "" && !seen[strings.ToLower(s)] && len(items) < 10 {
			seen[strings.ToLower(s)] = true
			items = append(items, s)
		}
	}
	ctx := r.Context()
	p := library.ListParams{Q: q, Limit: 8, Origin: "all"}
	if ar, err := d.Library.ListArtists(ctx, library.ListParams{Q: q, Limit: 3, Origin: "all"}); err == nil {
		for _, a := range ar.Items {
			add(a.Name)
		}
	}
	if tr, err := d.Library.ListTracks(ctx, p); err == nil {
		for _, t := range tr.Items {
			add(t.Title)
		}
	}
	if al, err := d.Library.ListAlbums(ctx, library.ListParams{Q: q, Limit: 4, Origin: "all"}); err == nil {
		for _, a := range al.Items {
			add(a.Title)
		}
	}
	w.Header().Set("Cache-Control", "private, max-age=30")
	writeJSON(w, http.StatusOK, map[string]any{"q": q, "items": items})
}
