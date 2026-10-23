package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"slices"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
	"github.com/my-name-is-alan/yimeng/core/internal/online"
)

const prefSourceSwitch = "source_switch"

// switchPrefs loads the caller's source-switch preference (D50).
func (d Deps) switchPrefs(ctx context.Context) online.SwitchPrefs {
	p := online.DefaultSwitch
	if d.UserData == nil {
		return p
	}
	dev, ok := DeviceFrom(ctx)
	if !ok {
		return p
	}
	_, _ = d.UserData.GetPref(ctx, ownerKey(dev), prefSourceSwitch, &p)
	if p.Priority == nil {
		p.Priority = []string{}
	}
	return p
}

func (d Deps) switchCtx(ctx context.Context) context.Context {
	return online.WithSwitch(ctx, d.switchPrefs(ctx))
}

// GET /api/v1/me/source-switch
func (d Deps) getSourceSwitch(w http.ResponseWriter, r *http.Request) {
	p := d.switchPrefs(r.Context())
	avail := []string{}
	if d.onlineFor(r.Context()) != nil {
		for _, id := range d.onlineFor(r.Context()).Reg.EnabledIDs() {
			avail = append(avail, id)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": p.Enabled, "priority": p.Priority, "platforms": avail})
}

// PUT /api/v1/me/source-switch {enabled, priority}
func (d Deps) putSourceSwitch(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Enabled  *bool    `json:"enabled"`
		Priority []string `json:"priority"`
	}
	if err := decodeBody(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	p := d.switchPrefs(r.Context())
	if b.Enabled != nil {
		p.Enabled = *b.Enabled
	}
	if b.Priority != nil {
		if len(b.Priority) > 16 {
			writeErr(w, http.StatusBadRequest, "invalid_body", "too many platforms")
			return
		}
		seen := map[string]bool{}
		clean := []string{}
		for _, id := range b.Priority {
			if id == "" || id == library.OriginLocal || len(id) > 32 || seen[id] {
				continue
			}
			if d.onlineFor(r.Context()) != nil && !slices.Contains(d.onlineFor(r.Context()).Reg.IDs(), id) {
				writeErr(w, http.StatusBadRequest, "unknown_platform", "unknown platform: "+id)
				return
			}
			seen[id] = true
			clean = append(clean, id)
		}
		p.Priority = clean
	}
	dev, _ := DeviceFrom(r.Context())
	if err := d.UserData.SetPref(r.Context(), ownerKey(dev), prefSourceSwitch, p); err != nil {
		d.internal(w, "prefs", err)
		return
	}
	d.getSourceSwitch(w, r)
}

// GET /api/v1/tracks/{id}/alternatives — the same song on other platforms.
func (d Deps) trackAlternatives(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if d.onlineFor(r.Context()) == nil || !library.IsExternalID(id) {
		writeJSON(w, http.StatusOK, map[string]any{"items": []online.Alternative{}})
		return
	}
	alts, err := d.onlineFor(r.Context()).Alternatives(d.switchCtx(r.Context()), id)
	if err != nil {
		d.platformErr(w, "alternatives", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": alts})
}

// alternativesFor is embedded in online track detail when the track is not
// fully playable on its own platform (or ?alternatives=1).
func (d Deps) alternativesFor(r *http.Request, id string, playable online.Entitlement) []online.Alternative {
	if d.onlineFor(r.Context()) == nil || !library.IsExternalID(id) {
		return nil
	}
	if playable == online.EntFull && r.URL.Query().Get("alternatives") != "1" {
		return nil
	}
	alts, err := d.onlineFor(r.Context()).Alternatives(d.switchCtx(r.Context()), id)
	if err != nil {
		return nil
	}
	return alts
}

// withFields marshals v and adds extra top-level fields (nil values skipped);
// works for types with their own MarshalJSON.
func withFields(v any, extra map[string]any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	m := map[string]json.RawMessage{}
	if json.Unmarshal(b, &m) != nil {
		return v
	}
	for k, x := range extra {
		if x == nil || reflect.ValueOf(x).Kind() == reflect.Slice && reflect.ValueOf(x).IsNil() {
			continue
		}
		if xb, err := json.Marshal(x); err == nil {
			m[k] = xb
		}
	}
	return m
}
