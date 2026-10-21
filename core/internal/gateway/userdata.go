package gateway

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/userdata"
)

func (d Deps) userErr(w http.ResponseWriter, what string, err error) {
	switch {
	case errors.Is(err, userdata.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not_found", what+" not found")
	case errors.Is(err, userdata.ErrBad):
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
	default:
		d.internal(w, what, err)
	}
}

func intQ(r *http.Request, k string) int {
	n, _ := strconv.Atoi(r.URL.Query().Get(k))
	return n
}

// scrobble records a play reported by a client that played the track itself.
func (d Deps) scrobble(w http.ResponseWriter, r *http.Request) {
	var b struct {
		TrackID    string     `json:"track_id"`
		PlayedAt   *time.Time `json:"played_at"`
		DurationMs int64      `json:"duration_ms"`
		Completed  bool       `json:"completed"`
	}
	if err := decodeBody(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	dev, _ := DeviceFrom(r.Context())
	p := userdata.Play{TrackID: b.TrackID, PlayerID: dev.ID, DurationMs: b.DurationMs, Completed: b.Completed, Source: "scrobble"}
	if b.PlayedAt != nil {
		p.PlayedAt = *b.PlayedAt
	}
	if err := d.UserData.Record(r.Context(), p); err != nil {
		d.userErr(w, "track", err)
		return
	}
	d.Hub.Publish("history.played", map[string]any{"track_id": p.TrackID, "player_id": p.PlayerID})
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) history(w http.ResponseWriter, r *http.Request) {
	items, total, err := d.UserData.History(r.Context(), r.URL.Query().Get("player_id"), intQ(r, "limit"), intQ(r, "offset"))
	if err != nil {
		d.userErr(w, "history", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

func (d Deps) historyTop(w http.ResponseWriter, r *http.Request) {
	var since time.Time
	switch p := r.URL.Query().Get("period"); p {
	case "", "all":
	case "7d", "30d", "90d", "365d":
		n, _ := strconv.Atoi(p[:len(p)-1])
		since = time.Now().AddDate(0, 0, -n)
	default:
		writeErr(w, http.StatusBadRequest, "invalid_query", "period must be 7d|30d|90d|365d|all")
		return
	}
	items, err := d.UserData.Top(r.Context(), r.URL.Query().Get("type"), since, intQ(r, "limit"))
	if err != nil {
		d.userErr(w, "top", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (d Deps) favorites(w http.ResponseWriter, r *http.Request) {
	items, err := d.UserData.Favorites(r.Context(), r.URL.Query().Get("type"), intQ(r, "limit"), intQ(r, "offset"))
	if err != nil {
		d.userErr(w, "favorites", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (d Deps) setFavorite(fav bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := d.UserData.SetFavorite(r.Context(), r.PathValue("type"), r.PathValue("id"), fav); err != nil {
			d.userErr(w, r.PathValue("type"), err)
			return
		}
		d.Hub.Publish("favorite.changed", map[string]any{"type": r.PathValue("type"), "id": r.PathValue("id"), "favorite": fav})
		w.WriteHeader(http.StatusNoContent)
	}
}

func (d Deps) setRating(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Rating *int `json:"rating"`
	}
	if err := decodeBody(r, &b); err != nil || b.Rating == nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", "rating (0-5) required")
		return
	}
	if err := d.UserData.SetRating(r.Context(), r.PathValue("type"), r.PathValue("id"), *b.Rating); err != nil {
		d.userErr(w, r.PathValue("type"), err)
		return
	}
	d.Hub.Publish("rating.changed", map[string]any{"type": r.PathValue("type"), "id": r.PathValue("id"), "rating": *b.Rating})
	w.WriteHeader(http.StatusNoContent)
}
