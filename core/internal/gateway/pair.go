package gateway

import (
	"errors"
	"net/http"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/auth"
)

func (d Deps) pairRequest(w http.ResponseWriter, r *http.Request) {
	code, exp, err := d.Auth.RequestCode(clientIP(r))
	switch {
	case errors.Is(err, auth.ErrRateLimited), errors.Is(err, auth.ErrTooManyCodes):
		w.Header().Set("Retry-After", "60")
		writeErr(w, http.StatusTooManyRequests, "rate_limited", err.Error())
		return
	case err != nil:
		d.internal(w, "pair request", err)
		return
	}
	// The code goes ONLY to the server log (docker logs) — whoever can read
	// the NAS console is the owner. Never returned in the response.
	d.Log.Warn("PAIRING CODE (enter it on the new device)", "code", code, "expires_at", exp.Format(time.RFC3339), "requested_by", clientIP(r))
	writeJSON(w, http.StatusAccepted, map[string]any{"expires_at": exp.UTC(), "message": "pairing code printed in the Core log"})
}

func (d Deps) newPairCode(w http.ResponseWriter, r *http.Request) {
	code, exp, err := d.Auth.NewCode()
	if errors.Is(err, auth.ErrTooManyCodes) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited", err.Error())
		return
	}
	if err != nil {
		d.internal(w, "pair code", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, map[string]any{"code": code, "expires_at": exp.UTC()})
}

type pairBody struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	DeviceType string `json:"device_type,omitempty"`
}

func (d Deps) pair(w http.ResponseWriter, r *http.Request) {
	var b pairBody
	if err := decodeBody(r, &b); err != nil || b.Code == "" || len(b.Code) > 32 {
		writeErr(w, http.StatusBadRequest, "invalid_body", "code and name are required")
		return
	}
	dev, tok, err := d.Auth.Pair(r.Context(), clientIP(r), b.Code, b.Name, b.DeviceType)
	switch {
	case errors.Is(err, auth.ErrRateLimited):
		w.Header().Set("Retry-After", "60")
		writeErr(w, http.StatusTooManyRequests, "rate_limited", "too many attempts; try again later")
	case errors.Is(err, auth.ErrInvalidCode):
		writeErr(w, http.StatusUnauthorized, "invalid_code", "invalid or expired pairing code")
	case err != nil:
		d.internal(w, "pair", err)
	default:
		d.Log.Info("device paired", "device", dev.ID, "name", dev.Name)
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusCreated, map[string]any{"device": dev, "token": tok})
	}
}

func (d Deps) listDevices(w http.ResponseWriter, r *http.Request) {
	devs, err := d.Auth.ListDevices(r.Context())
	if err != nil {
		d.internal(w, "list devices", err)
		return
	}
	if me, _ := DeviceFrom(r.Context()); !me.Admin {
		// non-admins only see their own sessions (same user, or the device itself)
		mine := devs[:0]
		for _, dv := range devs {
			if sameOwner(me, dv) {
				mine = append(mine, dv)
			}
		}
		devs = mine
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": devs})
}

func (d Deps) me(w http.ResponseWriter, r *http.Request) {
	dev, _ := DeviceFrom(r.Context())
	writeJSON(w, http.StatusOK, dev)
}

func (d Deps) revokeDevice(w http.ResponseWriter, r *http.Request) {
	if me, _ := DeviceFrom(r.Context()); !me.Admin {
		// non-admins may only sign out their own sessions
		target, err := d.Auth.DeviceByID(r.Context(), r.PathValue("id"))
		if err != nil || !sameOwner(me, target) {
			writeErr(w, http.StatusNotFound, "not_found", "device not found")
			return
		}
	}
	err := d.Auth.Revoke(r.Context(), r.PathValue("id"))
	switch {
	case errors.Is(err, auth.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not_found", "device not found")
	case err != nil:
		d.internal(w, "revoke device", err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
