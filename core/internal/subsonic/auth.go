package subsonic

import (
	"encoding/hex"
	"github.com/my-name-is-alan/yimeng/core/internal/owner"
	"net/http"
	"strings"

	"github.com/my-name-is-alan/yimeng/core/internal/auth"
)

// Auth resolves the paired device from Subsonic credentials.
// Supported: p=<ymd_token>, p=enc:<hex(token)>, or Authorization Bearer.
// t/s (token+salt) is rejected — see package comment.
func Auth(svc *auth.Service, r *http.Request) (auth.Device, error) {
	if h := r.Header.Get("Authorization"); len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return svc.Authenticate(r.Context(), strings.TrimSpace(h[7:]))
	}
	q := r.URL.Query()
	if q.Get("t") != "" || q.Get("s") != "" {
		return auth.Device{}, errTokenSalt
	}
	p := q.Get("p")
	if len(p) > 4 && strings.EqualFold(p[:4], "enc:") {
		raw, err := hex.DecodeString(p[4:])
		if err != nil {
			return auth.Device{}, auth.ErrUnauthorized
		}
		p = string(raw)
	}
	if p == "" {
		return auth.Device{}, auth.ErrUnauthorized
	}
	return svc.Authenticate(r.Context(), p)
}

type tokSaltErr struct{}

func (tokSaltErr) Error() string {
	return "subsonic token+salt (t/s) is not supported; use p=<device token> (or enc:hex)"
}

var errTokenSalt = tokSaltErr{}

func withAuth(svc *auth.Service, next func(http.ResponseWriter, *http.Request, auth.Device)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeErr(w, r, 40, "auth unavailable")
			return
		}
		dev, err := Auth(svc, r)
		if err != nil {
			if _, ok := err.(tokSaltErr); ok {
				writeErr(w, r, 41, err.Error())
				return
			}
			writeErr(w, r, 40, "wrong username or password")
			return
		}
		next(w, r.WithContext(owner.With(r.Context(), dev.UserID, dev.Admin)), dev)
	}
}
