package gateway

import (
	"net/http"
	"strings"

	"github.com/my-name-is-alan/yimeng/core/internal/online"
)

// APIVersion is the app API contract version (bumped on breaking changes).
const APIVersion = "1.1"

type platformStatus struct {
	Origin       string   `json:"origin"`
	Name         string   `json:"name"`
	Enabled      bool     `json:"enabled"`
	LoggedIn     bool     `json:"logged_in"`
	Nickname     string   `json:"nickname,omitempty"`   // authenticated callers only
	AvatarURL    string   `json:"avatar_url,omitempty"` // platform account avatar; authenticated callers only
	VIP          *bool    `json:"vip,omitempty"`        // authenticated callers only
	Capabilities []string `json:"capabilities"`
}

// GET /api/v1/server/info — public. Unauthenticated callers get the safe
// subset (no nicknames); a valid bearer adds account details.
func (d Deps) serverInfo(w http.ResponseWriter, r *http.Request) {
	authed := false
	if h := r.Header.Get("Authorization"); len(h) > 7 && strings.EqualFold(h[:7], "bearer ") && d.Auth != nil {
		_, err := d.Auth.Authenticate(r.Context(), strings.TrimSpace(h[7:]))
		authed = err == nil
	}
	name := d.ServerName
	if name == "" {
		name = "Yimeng"
	}
	features := map[string]bool{
		"online_sources": d.Online != nil && len(d.Online.Reg.EnabledIDs()) > 0,
		"dream_metadata": true, // cover colours + platform/genre tags only (no audio analysis)
		"lyrics":         true,
		"word_lyrics":    true, // merged lyrics carry words[] when a source has word timing
		"transcoding":    d.Transcoder != nil && len(d.Transcoder.Formats()) > 1,
		"events":         true, // /ws and SSE /api/v1/events
		"signed_urls":    d.Auth != nil,
		"progress_sync":  d.UserData != nil,
		"charts":         d.Online != nil,
		"ws":             true,
		"sse":            true,
	}
	plats := []platformStatus{}
	if d.Online != nil {
		for _, id := range d.Online.Reg.IDs() {
			st, err := d.Online.Reg.Status(r.Context(), id)
			if err != nil {
				continue
			}
			ps := platformStatus{Origin: id, Name: st.Manifest.Name, Enabled: st.Enabled, LoggedIn: st.LoggedIn, Capabilities: []string{}}
			ps.Capabilities = appCapabilities(st.Manifest.Capabilities)
			if authed && st.Account != nil {
				ps.Nickname = st.Account.Nickname
				ps.AvatarURL = httpsURL(st.Account.Avatar)
				v := st.Account.VIP
				ps.VIP = &v
			}
			plats = append(plats, ps)
		}
	}
	w.Header().Set("Cache-Control", "no-cache")
	writeJSON(w, http.StatusOK, map[string]any{
		"name": name, "server_name": name, "version": d.Version, "api_version": APIVersion, "schema_version": d.SchemaVersion,
		"features": features, "platforms": plats, "authenticated": authed,
	})
}

// appCapabilities maps plugin capabilities to the app's names:
// daily, fm, recommend_playlists, user_playlists, charts, lyrics, search, stream.
func appCapabilities(cs []online.Capability) []string {
	out := []string{}
	for _, c := range cs {
		switch c {
		case online.CapRecommend:
			out = append(out, "daily", "recommend_playlists")
		case online.CapDiscover:
			// discovery categories: not an app capability yet
		default:
			out = append(out, string(c))
		}
	}
	return out
}

// httpsURL upgrades platform CDN links so apps (ATS) can load them.
func httpsURL(u string) string {
	if strings.HasPrefix(u, "http://") {
		return "https://" + u[len("http://"):]
	}
	return u
}
