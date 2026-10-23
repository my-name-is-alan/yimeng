package subsonic

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"github.com/my-name-is-alan/yimeng/core/internal/auth"
	"github.com/my-name-is-alan/yimeng/core/internal/owner"
)

// Auth resolves Subsonic credentials (D59), in order of preference:
//
//  1. apiKey=<yk_…> (OpenSubsonic apiKeyAuthentication; u must be absent → 43)
//  2. Authorization: Bearer <device token | yk_ key>
//  3. u + t + s where the password is a per-account *app password*
//     (t = md5(app_password + s)); never the login password
//  4. u + p=<app password> (or enc:hex) — only if that app password allows plaintext
//  5. legacy: p=<paired device token> (ymd_…), any u
type authErr struct {
	code int
	msg  string
}

func (e authErr) Error() string { return e.msg }

var (
	errWrong     = authErr{40, "wrong username or password"}
	errBothKinds = authErr{43, "multiple conflicting authentication mechanisms provided"}
	errKeyBad    = authErr{44, "invalid API key"}
	errLocked    = authErr{40, "too many failed attempts; try again later"}
	errPlain     = authErr{41, "plaintext passwords are disabled for this app password; use token authentication (t+s) or apiKey"}
)

func (h *Handler) authenticate(r *http.Request) (auth.Device, error) {
	svc := h.Auth
	q := r.URL.Query()
	ctx := r.Context()
	if k := q.Get("apiKey"); k != "" {
		if q.Get("u") != "" || q.Get("t") != "" || q.Get("p") != "" {
			return auth.Device{}, errBothKinds
		}
		if !strings.HasPrefix(k, "yk_") {
			return auth.Device{}, errKeyBad
		}
		dev, err := svc.AuthenticateAPIKey(ctx, k)
		if err != nil {
			h.authFailed(ctx, "apikey", r)
			return auth.Device{}, errKeyBad
		}
		return dev, nil
	}
	if a := r.Header.Get("Authorization"); len(a) > 7 && strings.EqualFold(a[:7], "bearer ") {
		dev, err := svc.Authenticate(ctx, strings.TrimSpace(a[7:]))
		if err != nil {
			return auth.Device{}, errWrong
		}
		return dev, nil
	}
	u, t, s, p := q.Get("u"), q.Get("t"), q.Get("s"), q.Get("p")
	if len(p) > 4 && strings.EqualFold(p[:4], "enc:") {
		raw, err := hex.DecodeString(p[4:])
		if err != nil {
			return auth.Device{}, errWrong
		}
		p = string(raw)
	}
	if t == "" && strings.HasPrefix(p, "ymd_") { // legacy: device token as password
		dev, err := svc.Authenticate(ctx, p)
		if err != nil {
			return auth.Device{}, errWrong
		}
		return dev, nil
	}
	if u == "" || (t == "" && p == "") || (t != "" && s == "") {
		return auth.Device{}, authErr{10, "required parameter is missing (apiKey, or u with t+s)"}
	}
	dev, locked, err := svc.SubsonicPasswordAuth(ctx, u, t, s, p, h.ip(r))
	switch {
	case locked:
		h.authFailed(ctx, u, r, true)
		return auth.Device{}, errLocked
	case errors.Is(err, auth.ErrPlainRefused):
		return auth.Device{}, errPlain
	case err != nil:
		h.authFailed(ctx, u, r)
		return auth.Device{}, errWrong
	}
	return dev, nil
}

func (h *Handler) ip(r *http.Request) string {
	if h.ClientIP != nil {
		return h.ClientIP(r)
	}
	return r.RemoteAddr
}

func (h *Handler) authFailed(ctx context.Context, user string, r *http.Request, locked ...bool) {
	if h.OnAuthFail != nil {
		h.OnAuthFail(ctx, "subsonic:"+user, h.ip(r), len(locked) > 0 && locked[0])
	}
}

// withAuth authenticates, enforces the endpoint's scope and installs the
// owner/device context. POST form bodies (OpenSubsonic formPost) are merged
// into the query so handlers read one place.
func (h *Handler) withAuth(scope string, next func(http.ResponseWriter, *http.Request, auth.Device)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
			if err := r.ParseForm(); err != nil {
				writeErr(w, r, 0, "bad form body")
				return
			}
			r.URL.RawQuery = r.Form.Encode()
		}
		if h.Auth == nil {
			writeErr(w, r, 0, "auth unavailable")
			return
		}
		dev, err := h.authenticate(r)
		if err != nil {
			var ae authErr
			if errors.As(err, &ae) {
				writeErr(w, r, ae.code, ae.msg)
				return
			}
			writeErr(w, r, 40, "wrong username or password")
			return
		}
		if dev.Via != "" && !dev.HasScope(scope) {
			writeErr(w, r, 50, "credential lacks the "+scope+" scope")
			return
		}
		ctx := owner.With(r.Context(), dev.UserID, dev.Admin)
		if h.WithDevice != nil {
			ctx = h.WithDevice(ctx, dev)
		}
		if h.ScopeRequest != nil {
			h.ScopeRequest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { next(w, r, dev) })).ServeHTTP(w, r.WithContext(ctx))
			return
		}
		next(w, r.WithContext(ctx), dev)
	}
}
