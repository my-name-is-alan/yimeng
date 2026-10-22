package gateway

import (
	"errors"
	"github.com/my-name-is-alan/yimeng/core/internal/runtimecfg"
	"net/http"
	"strconv"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/lastfm"
	"github.com/my-name-is-alan/yimeng/core/internal/userdata"
)

// GET /api/v1/me/report?period=month|year&year=&month=&tz=&top= (D55)
func (d Deps) getReport(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	loc := d.reportLocation()
	if tz := q.Get("tz"); tz != "" {
		l, err := time.LoadLocation(tz)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_query", "tz must be an IANA zone such as Asia/Shanghai")
			return
		}
		loc = l
	}
	now := time.Now().In(loc)
	year, month := now.Year(), int(now.Month())
	atoi := func(k string, def int) (int, bool) {
		v := q.Get(k)
		if v == "" {
			return def, true
		}
		n, err := strconv.Atoi(v)
		return n, err == nil
	}
	var ok bool
	if year, ok = atoi("year", year); !ok {
		writeErr(w, http.StatusBadRequest, "invalid_query", "year must be a number")
		return
	}
	if month, ok = atoi("month", month); !ok || month < 1 || month > 12 {
		writeErr(w, http.StatusBadRequest, "invalid_query", "month must be 1..12")
		return
	}
	switch q.Get("period") {
	case "", "month":
	case "year":
		month = 0
	default:
		writeErr(w, http.StatusBadRequest, "invalid_query", "period must be month or year")
		return
	}
	top, _ := atoi("top", 10)
	rep, err := d.UserData.ListeningReport(r.Context(), year, month, loc, top)
	if errors.Is(err, userdata.ErrBad) {
		writeErr(w, http.StatusBadRequest, "invalid_query", err.Error())
		return
	}
	if err != nil {
		d.internal(w, "report", err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

func (d Deps) lastfmUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	if d.LastFM == nil {
		writeErr(w, http.StatusNotImplemented, "not_configured", "Last.fm is not available")
		return "", false
	}
	dev, _ := DeviceFrom(r.Context())
	if dev.UserID == "" {
		writeErr(w, http.StatusForbidden, "account_required", "sign in with an account to use Last.fm")
		return "", false
	}
	return dev.UserID, true
}

func (d Deps) lastfmErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, lastfm.ErrInvalidKeys):
		writeErr(w, http.StatusBadRequest, "invalid_body", "api_key and api_secret must be 32-character hex strings")
	case errors.Is(err, lastfm.ErrNoKeys):
		writeErr(w, http.StatusConflict, "lastfm_no_keys", "save your Last.fm API key and secret first")
	case errors.Is(err, lastfm.ErrNoToken):
		writeErr(w, http.StatusConflict, "lastfm_no_token", "start authorization first")
	case errors.Is(err, lastfm.ErrNotAuthed):
		writeErr(w, http.StatusConflict, "lastfm_not_authorized", "authorization not granted on last.fm yet")
	default:
		var ae *lastfm.APIError
		if errors.As(err, &ae) {
			writeErr(w, http.StatusBadGateway, "lastfm_error", ae.Message)
			return
		}
		d.Log.Warn("lastfm request failed", "err", err.Error())
		writeErr(w, http.StatusBadGateway, "lastfm_unreachable", "could not reach Last.fm")
	}
}

// GET /api/v1/me/lastfm
func (d Deps) getLastFM(w http.ResponseWriter, r *http.Request) {
	uid, ok := d.lastfmUser(w, r)
	if !ok {
		return
	}
	st, err := d.LastFM.Status(r.Context(), uid)
	if err != nil {
		d.internal(w, "lastfm", err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// PUT /api/v1/me/lastfm/keys {api_key, api_secret} — write-only.
func (d Deps) putLastFMKeys(w http.ResponseWriter, r *http.Request) {
	uid, ok := d.lastfmUser(w, r)
	if !ok {
		return
	}
	var b struct {
		APIKey    string `json:"api_key"`
		APISecret string `json:"api_secret"`
	}
	if err := decodeBody(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", "expected {api_key, api_secret}")
		return
	}
	if err := d.LastFM.SetKeys(r.Context(), uid, b.APIKey, b.APISecret); err != nil {
		d.lastfmErr(w, err)
		return
	}
	d.getLastFM(w, r)
}

// PUT /api/v1/me/lastfm {enabled}
func (d Deps) putLastFM(w http.ResponseWriter, r *http.Request) {
	uid, ok := d.lastfmUser(w, r)
	if !ok {
		return
	}
	var b struct {
		Enabled *bool `json:"enabled"`
	}
	if err := decodeBody(r, &b); err != nil || b.Enabled == nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", "expected {enabled}")
		return
	}
	if err := d.LastFM.SetEnabled(r.Context(), uid, *b.Enabled); err != nil {
		d.lastfmErr(w, err)
		return
	}
	d.getLastFM(w, r)
}

// POST /api/v1/me/lastfm/auth → {auth_url}
func (d Deps) startLastFMAuth(w http.ResponseWriter, r *http.Request) {
	uid, ok := d.lastfmUser(w, r)
	if !ok {
		return
	}
	u, err := d.LastFM.StartAuth(r.Context(), uid)
	if err != nil {
		d.lastfmErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"auth_url": u})
}

// POST /api/v1/me/lastfm/auth/complete
func (d Deps) completeLastFMAuth(w http.ResponseWriter, r *http.Request) {
	uid, ok := d.lastfmUser(w, r)
	if !ok {
		return
	}
	st, err := d.LastFM.CompleteAuth(r.Context(), uid)
	if err != nil {
		d.lastfmErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// DELETE /api/v1/me/lastfm
func (d Deps) deleteLastFM(w http.ResponseWriter, r *http.Request) {
	uid, ok := d.lastfmUser(w, r)
	if !ok {
		return
	}
	if err := d.LastFM.Disconnect(r.Context(), uid); err != nil {
		d.internal(w, "lastfm", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// reportLocation is the server's configured zone (YIMENG_TZ / runtime
// setting time_zone, default Asia/Shanghai) — never the host's "Local".
func (d Deps) reportLocation() *time.Location {
	if d.ServerSettings != nil {
		return d.ServerSettings.Get().Location()
	}
	return runtimecfg.Settings{}.Location()
}
