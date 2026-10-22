package gateway

// Open developer API (D59): scoped API keys, Subsonic app passwords, CORS.

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/auth"
)

// scopeFor returns the scope a request needs when made with an API key, or
// "" when API keys may not call it at all.
func scopeFor(r *http.Request) string {
	p := r.URL.Path
	switch {
	// credential management, sessions and secrets: never via API keys
	case strings.HasPrefix(p, "/api/developer/"), strings.HasPrefix(p, "/api/auth/"), strings.HasPrefix(p, "/api/pair"),
		strings.HasPrefix(p, "/api/me/lastfm"), strings.HasPrefix(p, "/api/devices"):
		return ""
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		if strings.HasPrefix(p, "/api/stream/") || strings.HasPrefix(p, "/api/download/") || strings.HasSuffix(p, "/download") {
			return auth.ScopePlayback // audio bytes count as playback
		}
		return auth.ScopeRead
	}
	switch {
	case strings.HasPrefix(p, "/api/players"), strings.HasPrefix(p, "/api/scrobble"), strings.HasPrefix(p, "/api/progress"),
		strings.HasPrefix(p, "/api/rooms"), strings.HasPrefix(p, "/api/queue"), strings.HasPrefix(p, "/api/me/playback"):
		return auth.ScopePlayback
	case strings.HasPrefix(p, "/api/users"), strings.HasPrefix(p, "/api/system"), strings.HasPrefix(p, "/api/settings"),
		strings.HasPrefix(p, "/api/sources"), strings.HasPrefix(p, "/api/plugins"), strings.HasPrefix(p, "/api/library/scan"),
		strings.HasPrefix(p, "/api/security"):
		return auth.ScopeAdmin
	}
	return auth.ScopeWrite
}

// checkScope enforces API-key scopes; true = allowed.
func checkScope(w http.ResponseWriter, r *http.Request, dev auth.Device) bool {
	if dev.Via == "" {
		return true
	}
	if dev.Via != auth.KindAPIKey { // app passwords are for /rest only
		writeErr(w, http.StatusForbidden, "insufficient_scope", "app passwords only work with the Subsonic API (/rest)")
		return false
	}
	need := scopeFor(r)
	if need == "" {
		writeErr(w, http.StatusForbidden, "insufficient_scope", "this endpoint is not available to API keys")
		return false
	}
	if !dev.HasScope(need) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="yimeng", error="insufficient_scope", scope="`+need+`"`)
		writeErr(w, http.StatusForbidden, "insufficient_scope", "API key lacks the "+need+" scope")
		return false
	}
	return true
}

// cors applies the configured CORS policy to /api and /rest. Credentials are
// bearer tokens (never cookies), so Allow-Credentials is never sent.
func (d Deps) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		p := r.URL.Path
		api := strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/rest/") || p == "/ws"
		if origin == "" || !api || d.ServerSettings == nil {
			next.ServeHTTP(w, r)
			return
		}
		allowed := d.ServerSettings.Get().CORSOrigins
		ok := slices.Contains(allowed, "*") || slices.Contains(allowed, strings.TrimRight(origin, "/"))
		if ok {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Add("Vary", "Origin")
			h.Set("Access-Control-Expose-Headers", "X-Yimeng-API-Version, X-Yimeng-Fallback, X-Yimeng-Play-Id, Content-Range, Content-Length, Retry-After")
		}
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			if !ok {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			h := w.Header()
			h.Set("Access-Control-Allow-Methods", "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Range, If-None-Match, If-Match")
			h.Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// apiVersionHeader tags every /api response with the contract version.
func apiVersionHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("X-Yimeng-API-Version", APIVersion)
		}
		next.ServeHTTP(w, r)
	})
}

func (d Deps) credOwner(w http.ResponseWriter, r *http.Request) (auth.Device, bool) {
	dev, _ := DeviceFrom(r.Context())
	if dev.UserID == "" {
		writeErr(w, http.StatusForbidden, "account_required", "sign in with an account to manage developer credentials")
		return dev, false
	}
	return dev, true
}

// GET /api/v1/developer/credentials[?all=1 (admin)]
func (d Deps) listCredentials(w http.ResponseWriter, r *http.Request) {
	dev, ok := d.credOwner(w, r)
	if !ok {
		return
	}
	uid := dev.UserID
	if r.URL.Query().Get("all") == "1" && dev.Admin {
		uid = ""
	}
	items, err := d.Auth.ListCredentials(r.Context(), uid)
	if err != nil {
		d.internal(w, "credentials", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "scopes": auth.AllScopes})
}

// POST /api/v1/developer/credentials {kind, name, scopes, allow_plain?, expires_in_days?}
func (d Deps) createCredential(w http.ResponseWriter, r *http.Request) {
	dev, ok := d.credOwner(w, r)
	if !ok {
		return
	}
	var b struct {
		Kind          string   `json:"kind"`
		Name          string   `json:"name"`
		Scopes        []string `json:"scopes"`
		AllowPlain    bool     `json:"allow_plain"`
		ExpiresInDays int      `json:"expires_in_days"`
	}
	if err := decodeBody(r, &b); err != nil || b.ExpiresInDays < 0 || b.ExpiresInDays > 3650 {
		writeErr(w, http.StatusBadRequest, "invalid_body", "expected {kind, name, scopes[], allow_plain?, expires_in_days? (0..3650)}")
		return
	}
	if b.Kind == auth.KindAppPassword && len(b.Scopes) == 0 {
		b.Scopes = []string{auth.ScopeRead, auth.ScopePlayback, auth.ScopeWrite}
	}
	c, secret, err := d.Auth.CreateCredential(r.Context(), auth.NewCredential{UserID: dev.UserID, Kind: b.Kind, Name: b.Name,
		Scopes: b.Scopes, AllowPlain: b.AllowPlain, TTL: time.Duration(b.ExpiresInDays) * 24 * time.Hour})
	switch {
	case errors.Is(err, auth.ErrInvalidScope):
		writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	case errors.Is(err, auth.ErrNoSecretStore):
		writeErr(w, http.StatusNotImplemented, "not_configured", "app passwords need the vault (master key)")
		return
	case err != nil:
		d.acctErr(w, "create credential", err)
		return
	}
	d.Log.Info("developer credential created", "id", c.ID, "kind", c.Kind, "user", c.UserID, "scopes", strings.Join(c.Scopes, " "))
	if d.Alerts != nil {
		d.Alerts.DevicePaired(r.Context(), c.UserID, c.ID, c.Name, c.Kind, clientIP(r))
	}
	writeJSON(w, http.StatusCreated, map[string]any{"credential": c, "secret": secret,
		"note": "the secret is shown only once; store it now"})
}

// DELETE /api/v1/developer/credentials/{id} (owner, or admin for any)
func (d Deps) revokeCredential(w http.ResponseWriter, r *http.Request) {
	dev, ok := d.credOwner(w, r)
	if !ok {
		return
	}
	uid := dev.UserID
	if dev.Admin {
		uid = ""
	}
	if err := d.Auth.RevokeCredential(r.Context(), r.PathValue("id"), uid); err != nil {
		if errors.Is(err, auth.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "not_found", "credential not found")
			return
		}
		d.internal(w, "revoke credential", err)
		return
	}
	d.Log.Info("developer credential revoked", "id", r.PathValue("id"), "by", dev.ID)
	w.WriteHeader(http.StatusNoContent)
}
