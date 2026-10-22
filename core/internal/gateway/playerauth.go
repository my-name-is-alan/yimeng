package gateway

import (
	"context"
	"net/http"
	"sync"

	"github.com/my-name-is-alan/yimeng/core/internal/auth"
	"github.com/my-name-is-alan/yimeng/core/internal/playback"
)

// canControl reports whether me may see/control player pid (D49): admin
// may control everything; a player backed by a paired device belongs to that
// device's account; a player with no device (server-side output) is shared.
func (d Deps) canControl(ctx context.Context, me auth.Device, pid string) bool {
	if me.Admin || pid == "" || pid == me.ID || d.Auth == nil {
		return true
	}
	dv, err := d.Auth.DeviceByID(ctx, pid)
	if err != nil {
		return true // not a device: shared output
	}
	if me.UserID == "" && dv.UserID == "" {
		return true // install without accounts: one shared household
	}
	return sameOwner(me, dv)
}

// ownPlayer wraps /players/{id}/… so other accounts' players are 404.
func (d Deps) ownPlayer(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		me, _ := DeviceFrom(r.Context())
		if !d.canControl(r.Context(), me, r.PathValue("id")) {
			writeErr(w, http.StatusNotFound, "not_found", "player not found")
			return
		}
		h(w, r)
	}
}

// filterStates keeps the players me may see.
func (d Deps) filterStates(ctx context.Context, me auth.Device, st []playback.State) []playback.State {
	out := st[:0]
	for _, s := range st {
		if d.canControl(ctx, me, s.PlayerID) {
			out = append(out, s)
		}
	}
	return out
}

// eventPlayer extracts the player id of a playback event (""=not one).
func eventPlayer(data any) string {
	switch v := data.(type) {
	case playback.State:
		return v.PlayerID
	case *playback.State:
		return v.PlayerID
	case playback.Queue:
		return v.PlayerID
	case *playback.Queue:
		return v.PlayerID
	case map[string]any:
		s, _ := v["player_id"].(string)
		return s
	case map[string]string:
		return v["from"]
	}
	return ""
}

// playerFilter memoizes canControl for one event stream connection.
func (d Deps) playerFilter(ctx context.Context, me auth.Device) func(pid string) bool {
	var mu sync.Mutex
	seen := map[string]bool{}
	return func(pid string) bool {
		mu.Lock()
		defer mu.Unlock()
		if v, ok := seen[pid]; ok {
			return v
		}
		v := d.canControl(ctx, me, pid)
		seen[pid] = v
		return v
	}
}
