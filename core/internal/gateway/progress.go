package gateway

import (
	"net/http"
	"strings"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/auth"
	"github.com/my-name-is-alan/yimeng/core/internal/userdata"
)

// ownerKey scopes per-user data: the account, or the device for
// code-paired devices without an account.
func ownerKey(dev auth.Device) string {
	if dev.UserID != "" {
		return dev.UserID
	}
	return "dev:" + dev.ID
}

// PUT /api/v1/progress/{trackId} {position_ms, duration_ms?, finished?, updated_at?}
func (d Deps) putProgress(w http.ResponseWriter, r *http.Request) {
	var b struct {
		PositionMs int64     `json:"position_ms"`
		DurationMs int64     `json:"duration_ms"`
		Finished   bool      `json:"finished"`
		UpdatedAt  time.Time `json:"updated_at"`
	}
	if err := decodeBody(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	dev, _ := DeviceFrom(r.Context())
	p, err := d.UserData.SetProgress(r.Context(), ownerKey(dev), userdata.Progress{TrackID: r.PathValue("id"), PositionMs: b.PositionMs,
		DurationMs: b.DurationMs, Finished: b.Finished, DeviceID: dev.ID, UpdatedAt: b.UpdatedAt})
	if err != nil {
		d.userErr(w, "progress", err)
		return
	}
	d.publishUser(dev, "progress.changed", p)
	writeJSON(w, http.StatusOK, p)
}

// GET /api/v1/progress?ids=a,b (no ids = most recent 500)
func (d Deps) getProgress(w http.ResponseWriter, r *http.Request) {
	var ids []string
	for _, s := range strings.Split(r.URL.Query().Get("ids"), ",") {
		if s = strings.TrimSpace(s); s != "" {
			ids = append(ids, s)
		}
	}
	dev, _ := DeviceFrom(r.Context())
	items, err := d.UserData.GetProgress(r.Context(), ownerKey(dev), ids)
	if err != nil {
		d.internal(w, "progress", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (d Deps) deleteProgress(w http.ResponseWriter, r *http.Request) {
	dev, _ := DeviceFrom(r.Context())
	if err := d.UserData.DeleteProgress(r.Context(), ownerKey(dev), r.PathValue("id")); err != nil {
		d.internal(w, "progress", err)
		return
	}
	d.publishUser(dev, "progress.changed", map[string]any{"track_id": r.PathValue("id"), "deleted": true})
	w.WriteHeader(http.StatusNoContent)
}

// publishUser emits an event scoped to the calling user (see /ws, /events).
func (d Deps) publishUser(dev auth.Device, typ string, data any) {
	d.Hub.Publish(typ, scopedEvent{Owner: ownerKey(dev), Origin: dev.ID, Data: data})
}

// scopedEvent wraps per-user data; the event streams only deliver it to
// sessions of the same owner and unwrap it (origin = sending device).
type scopedEvent struct {
	Owner  string `json:"-"`
	Origin string `json:"-"`
	Data   any    `json:"-"`
}

// eventFor unwraps a user-scoped event for dev; ok=false hides it.
func eventFor(dev auth.Device, data any) (any, bool) {
	se, ok := data.(scopedEvent)
	if !ok {
		return data, true
	}
	if se.Owner != ownerKey(dev) {
		return nil, false
	}
	return map[string]any{"origin_device": se.Origin, "item": se.Data}, true
}
