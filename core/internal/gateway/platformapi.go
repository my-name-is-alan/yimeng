package gateway

import (
	"errors"
	"net/http"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/online"
)

// Platform account APIs (D45). All of these are the logged-in platform
// account's own data, passed through as the platform serves it (Core has no
// recommender), cached by the platform cache, no unblocking.
//
//	GET  /api/v1/online/{origin}/daily
//	GET  /api/v1/online/{origin}/fm                    (404 not_supported if the platform has none)
//	POST /api/v1/online/{origin}/fm/feedback {track_id, action: like|trash}
//	GET  /api/v1/online/{origin}/recommend/playlists
//	GET  /api/v1/online/{origin}/user/playlists?type=created|collected

func (d Deps) platformErr(w http.ResponseWriter, what string, err error) {
	if errors.Is(err, online.ErrNeedsLogin) {
		// 404, not 401: a 401 would read as "your Core session expired".
		writeErr(w, http.StatusNotFound, "not_logged_in", "the platform account is not logged in on this Core")
		return
	}
	if errors.Is(err, online.ErrNotSupported) {
		writeErr(w, http.StatusNotFound, "not_supported", "this platform does not offer "+what)
		return
	}
	d.onlineErr(w, what, err)
}

func (d Deps) platformDaily(w http.ResponseWriter, r *http.Request) {
	o := r.PathValue("origin")
	ts, err := d.Online.RecommendTracks(r.Context(), o)
	if err != nil {
		d.platformErr(w, "daily recommendations", err)
		return
	}
	d.decorateOnlineTracks(ts)
	writeJSON(w, http.StatusOK, map[string]any{"origin": o, "date": time.Now().Format("2006-01-02"), "tracks": ts, "items": ts})
}

func (d Deps) platformFM(w http.ResponseWriter, r *http.Request) {
	o := r.PathValue("origin")
	ts, err := d.Online.FM(r.Context(), o)
	if err != nil {
		d.platformErr(w, "personal FM", err)
		return
	}
	d.decorateOnlineTracks(ts)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"origin": o, "items": ts})
}

func (d Deps) platformFMFeedback(w http.ResponseWriter, r *http.Request) {
	var b struct {
		TrackID string `json:"track_id"`
		Action  string `json:"action"`
	}
	if err := decodeBody(r, &b); err != nil || b.TrackID == "" || (b.Action != "like" && b.Action != "trash") {
		writeErr(w, http.StatusBadRequest, "invalid_body", "track_id and action (like|trash) are required")
		return
	}
	if err := d.Online.FMFeedback(r.Context(), r.PathValue("origin"), b.TrackID, b.Action); err != nil {
		d.platformErr(w, "personal FM", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) platformRecPlaylists(w http.ResponseWriter, r *http.Request) {
	o := r.PathValue("origin")
	if !d.Online.Reg.LoggedIn(o) {
		d.platformErr(w, "recommended playlists", online.ErrNeedsLogin)
		return
	}
	ps, err := d.Online.RecommendPlaylists(r.Context(), o)
	if err != nil {
		d.platformErr(w, "recommended playlists", err)
		return
	}
	d.decorateOnlinePlaylists(ps)
	writeJSON(w, http.StatusOK, map[string]any{"origin": o, "items": ps})
}

func (d Deps) platformUserPlaylists(w http.ResponseWriter, r *http.Request) {
	o := r.PathValue("origin")
	typ := r.URL.Query().Get("type")
	if typ != "" && typ != "created" && typ != "collected" {
		writeErr(w, http.StatusBadRequest, "invalid_query", "type must be created or collected")
		return
	}
	ps, err := d.Online.UserPlaylists(r.Context(), o)
	if err != nil {
		d.platformErr(w, "user playlists", err)
		return
	}
	out := make([]online.Playlist, 0, len(ps))
	for _, p := range ps {
		mine := p.Mine || p.Kind == "liked"
		if typ == "" || (typ == "created") == mine {
			out = append(out, p)
		}
	}
	d.decorateOnlinePlaylists(out)
	writeJSON(w, http.StatusOK, map[string]any{"origin": o, "type": typ, "items": out})
}

func (d Deps) platformSub(w http.ResponseWriter, r *http.Request) {
	switch r.PathValue("sub") {
	case "daily":
		d.platformDaily(w, r)
	case "fm":
		d.platformFM(w, r)
	default:
		writeErr(w, http.StatusNotFound, "not_found", "unknown platform resource")
	}
}
