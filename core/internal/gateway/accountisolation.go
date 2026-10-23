package gateway

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/my-name-is-alan/yimeng/core/internal/online"
	"github.com/my-name-is-alan/yimeng/core/internal/owner"
)

func (d Deps) onlineFor(ctx context.Context) *online.Service {
	if d.Accounts == nil {
		return d.Online
	}
	if s := online.FromContext(ctx); s != nil {
		return s
	}
	if s, err := d.Accounts.Public(ctx); err == nil {
		return s
	}
	return d.OnlinePublic // never fall back to the legacy administrator session
}

func personalMutation(r *http.Request) bool {
	p := r.URL.Path
	if !strings.HasPrefix(p, "/api/plugins/") {
		return false
	}
	return strings.Contains(p, "/login") || strings.Contains(p, "/accounts/") || strings.HasSuffix(p, "/session") || strings.HasSuffix(p, "/refresh")
}

func (d Deps) scopeOnline(next http.Handler) http.Handler {
	if d.Accounts == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dev, ok := DeviceFrom(r.Context())
		p := r.URL.Path
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		if dev.UserID == "" {
			if personalMutation(r) {
				d.onlineErr(w, "account", online.ErrIdentityRequired)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		// Long-lived requests must not prevent personal account changes. Their
		// audio path validates a separate grant/account snapshot for every request.
		if p == "/ws" || p == "/api/events" || strings.HasSuffix(p, "/events") || strings.HasPrefix(p, "/api/stream/") || strings.HasSuffix(p, "/download") || strings.HasPrefix(p, "/rest/stream") || strings.HasPrefix(p, "/rest/download") ||
			strings.HasPrefix(p, "/api/admin/stream-grants") || p == "/api/admin/platform-legacy/claim" {
			next.ServeHTTP(w, r)
			return
		}
		ctx, release, err := d.Accounts.Acquire(r.Context(), personalMutation(r))
		if err != nil {
			d.onlineErr(w, "personal account", err)
			return
		}
		defer release()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Existing plugin login URLs now manage only the authenticated user's accounts.
// On legacy test/embed setups without the account manager their old admin gate remains.
func (d Deps) personalOnly(h http.HandlerFunc) http.HandlerFunc {
	if d.Accounts == nil {
		return d.adminOnly(h)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		dev, ok := DeviceFrom(r.Context())
		if !ok || dev.UserID == "" || dev.Via != "" {
			writeErr(w, 403, "account_session_required", "sign in with a user session to manage personal platform accounts")
			return
		}
		if strings.Contains(r.URL.Path, "/login") || strings.HasSuffix(r.URL.Path, "/refresh") {
			if _, _, err := d.onlineFor(r.Context()).Reg.Enabled(r.PathValue("id")); err != nil {
				d.onlineErr(w, "plugin", err)
				return
			}
		}
		defer d.Accounts.SessionsChanged(r.Context(), dev.UserID)
		h(w, r)
	}
}

type privatePluginEvent struct {
	User string
	Data any
}

func (d Deps) pluginEvent(ctx context.Context, id string) {
	v := any(map[string]any{"id": id})
	if d.Accounts != nil {
		v = privatePluginEvent{owner.From(ctx).ID, v}
	}
	d.Hub.Publish("plugin.updated", v)
}

func (d Deps) mountAccountIsolation(mux *http.ServeMux) {
	if d.Accounts == nil {
		return
	}
	mux.HandleFunc("GET /api/me/platform-bindings", d.platformBindings)
	mux.HandleFunc("GET /api/admin/stream-grants", d.adminOnly(d.listStreamGrants))
	mux.HandleFunc("POST /api/admin/stream-grants", d.adminOnly(d.createStreamGrant))
	mux.HandleFunc("DELETE /api/admin/stream-grants/{grant}", d.adminOnly(d.revokeStreamGrant))
	mux.HandleFunc("POST /api/admin/platform-legacy/claim", d.adminOnly(d.claimLegacyPlatforms))
}

func (d Deps) platformBindings(w http.ResponseWriter, r *http.Request) {
	dev, _ := DeviceFrom(r.Context())
	grants, err := d.Accounts.Grants(r.Context())
	if err != nil {
		d.onlineErr(w, "bindings", err)
		return
	}
	mode := "shared_required"
	if dev.Admin {
		mode = "own"
	}
	writeJSON(w, 200, map[string]any{"stream_mode": mode, "grants": grants, "legacy_claim_required": dev.Admin && d.Accounts.LegacyPending(r.Context())})
}
func (d Deps) listStreamGrants(w http.ResponseWriter, r *http.Request) {
	items, err := d.Accounts.Grants(r.Context())
	if err != nil {
		d.onlineErr(w, "grants", err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}
func (d Deps) createStreamGrant(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Consumer string `json:"consumer_user_id"`
		Platform string `json:"platform"`
		Account  string `json:"account_id"`
		Label    string `json:"label"`
	}
	if err := decodeBody(r, &b); err != nil {
		writeErr(w, 400, "invalid_body", err.Error())
		return
	}
	g, err := d.Accounts.Grant(r.Context(), b.Consumer, b.Platform, b.Account, b.Label)
	if err != nil {
		d.onlineErr(w, "grant", err)
		return
	}
	d.Hub.Publish("stream-grant.updated", privatePluginEvent{g.ConsumerUserID, map[string]string{"platform": g.Platform}})
	writeJSON(w, 201, g)
}
func (d Deps) revokeStreamGrant(w http.ResponseWriter, r *http.Request) {
	if err := d.Accounts.Revoke(r.Context(), r.PathValue("grant")); err != nil {
		d.onlineErr(w, "revoke", err)
		return
	}
	w.WriteHeader(204)
}
func (d Deps) claimLegacyPlatforms(w http.ResponseWriter, r *http.Request) {
	if err := d.Accounts.MigrateLegacy(r.Context(), owner.From(r.Context()).ID); err != nil {
		d.onlineErr(w, "claim", err)
		return
	}
	if err := d.Accounts.ConfigChanged(); err != nil {
		d.internal(w, "platform config", err)
		return
	}
	writeJSON(w, 200, map[string]bool{"claimed": true})
}

func isolationError(err error) (int, string, string, bool) {
	switch {
	case errors.Is(err, online.ErrSharedNeedsLogin):
		return 409, "shared_account_needs_login", "共享平台账号需要管理员重新登录；不会改用你的个人账号取流", true
	case errors.Is(err, online.ErrSharedRequired):
		return 403, "shared_stream_required", "请管理员为你的账号授权一个固定取流账号", true
	case errors.Is(err, online.ErrGrantRevoked):
		return 403, "shared_stream_revoked", "共享取流授权已撤销或已变更", true
	case errors.Is(err, online.ErrIdentityRequired):
		return 403, "account_identity_required", "请先登录译梦用户账号", true
	case errors.Is(err, online.ErrGrantConflict):
		return 409, "stream_grant_conflict", "已有其他管理员的取流授权，请先撤销", true
	case errors.Is(err, online.ErrUnknownAccount):
		return 404, "unknown_account", "账号或授权不存在", true
	}
	return 0, "", "", false
}
