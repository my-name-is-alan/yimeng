package gateway

import (
	"net/http"

	"github.com/my-name-is-alan/yimeng/core/internal/auth"
)

// GET /api/v1/sessions — the caller's devices (same user, or just itself for a
// paired device) with presence and current playback state.
//
//	{items:[{device:{id,name,device_type,last_seen_at}, current, online, state|null}]}
func (d Deps) listSessions(w http.ResponseWriter, r *http.Request) {
	me, _ := DeviceFrom(r.Context())
	devs, err := d.Auth.ListDevices(r.Context())
	if err != nil {
		d.internal(w, "sessions", err)
		return
	}
	items := []map[string]any{}
	for _, dv := range devs {
		if !sameOwner(me, dv) {
			continue
		}
		it := map[string]any{"device": dv, "current": dv.ID == me.ID, "online": d.Presence.Online(dv.ID), "state": nil}
		if d.Playback != nil {
			if st, err := d.Playback.State(r.Context(), dv.ID); err == nil && st.Transport != "" {
				it["state"] = d.withOnline(st)
			}
		}
		items = append(items, it)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func sameOwner(a, b auth.Device) bool {
	if a.UserID != "" {
		return a.UserID == b.UserID
	}
	return a.ID == b.ID
}

// POST /api/v1/sessions/handoff {to, from?} — move playback (queue, track,
// position) from one of the caller's sessions (default: the caller) to another.
func (d Deps) handoff(w http.ResponseWriter, r *http.Request) {
	me, _ := DeviceFrom(r.Context())
	var b struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := decodeBody(r, &b); err != nil || b.To == "" {
		writeErr(w, http.StatusBadRequest, "invalid_body", "to (destination device id) required")
		return
	}
	if b.From == "" {
		b.From = me.ID
	}
	for _, id := range []string{b.From, b.To} {
		dv, err := d.Auth.DeviceByID(r.Context(), id)
		if err != nil || !(sameOwner(me, dv) || me.Admin) {
			writeErr(w, http.StatusNotFound, "not_found", "session not found: "+id)
			return
		}
	}
	st, err := d.Playback.Transfer(r.Context(), b.From, b.To)
	if err != nil {
		d.playbackResult(w, st, err)
		return
	}
	d.publishUser(me, "session.handoff", map[string]any{"from": b.From, "to": b.To})
	d.playbackResult(w, d.withOnline(st), nil)
}
