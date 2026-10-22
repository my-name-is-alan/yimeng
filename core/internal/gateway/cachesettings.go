package gateway

import "net/http"

// GET /api/settings/cache — platform cache stats (admin).
func (d *Deps) getCacheStats(w http.ResponseWriter, r *http.Request) {
	if d.Online == nil || d.Online.Cache == nil {
		writeErr(w, http.StatusNotFound, "not_supported", "online cache not configured")
		return
	}
	st, err := d.Online.Cache.Stats(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// DELETE /api/settings/cache?origin=&kind= — clear entries (admin).
func (d *Deps) clearCache(w http.ResponseWriter, r *http.Request) {
	if d.Online == nil || d.Online.Cache == nil {
		writeErr(w, http.StatusNotFound, "not_supported", "online cache not configured")
		return
	}
	n, err := d.Online.Cache.Clear(r.Context(), r.URL.Query().Get("origin"), r.URL.Query().Get("kind"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}
