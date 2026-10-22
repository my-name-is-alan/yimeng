package gateway

import (
	"bytes"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	_ "golang.org/x/image/webp"

	"github.com/my-name-is-alan/yimeng/core/internal/auth"
)

const (
	avatarMaxBytes = 1 << 20 // 1 MiB
	avatarMaxSide  = 4096
)

// GET /api/v1/me — the caller's account profile.
func (d Deps) getMe(w http.ResponseWriter, r *http.Request) {
	dev, _ := DeviceFrom(r.Context())
	if dev.UserID == "" {
		writeErr(w, http.StatusForbidden, "account_required", "this device is not signed in to an account")
		return
	}
	u, err := d.Auth.UserByID(r.Context(), dev.UserID)
	if err != nil {
		d.acctErr(w, "me", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": u.ID, "username": u.Username, "nickname": u.Nickname, "avatar_url": nullStr(u.AvatarURL), "role": u.Role})
}

func nullStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// PATCH /api/v1/me {nickname}
func (d Deps) patchMe(w http.ResponseWriter, r *http.Request) {
	dev, _ := DeviceFrom(r.Context())
	if dev.UserID == "" {
		writeErr(w, http.StatusForbidden, "account_required", "this device is not signed in to an account")
		return
	}
	var b struct {
		Nickname *string `json:"nickname"`
	}
	if err := decodeBody(r, &b); err != nil || b.Nickname == nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", "expected {nickname}")
		return
	}
	n := strings.TrimSpace(*b.Nickname)
	if utf8.RuneCountInString(n) > 32 || strings.ContainsAny(n, "\x00\r\n\t") {
		writeErr(w, http.StatusBadRequest, "invalid_body", "nickname: up to 32 characters, single line")
		return
	}
	if err := d.Auth.SetNickname(r.Context(), dev.UserID, n); err != nil {
		d.acctErr(w, "nickname", err)
		return
	}
	d.getMe(w, r)
}

// readAvatar validates an uploaded image: JPEG/PNG/WebP, ≤1 MiB, ≤4096 px.
func readAvatar(w http.ResponseWriter, r *http.Request) (string, []byte, bool) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, avatarMaxBytes+1))
	if err != nil || len(data) > avatarMaxBytes {
		writeErr(w, http.StatusRequestEntityTooLarge, "too_large", "avatar must be at most 1 MiB")
		return "", nil, false
	}
	if len(data) == 0 {
		writeErr(w, http.StatusBadRequest, "invalid_body", "send the image as the request body")
		return "", nil, false
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	mime := map[string]string{"jpeg": "image/jpeg", "png": "image/png", "webp": "image/webp"}[format]
	if err != nil || mime == "" {
		writeErr(w, http.StatusUnsupportedMediaType, "unsupported_type", "avatar must be JPEG, PNG or WebP")
		return "", nil, false
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > avatarMaxSide || cfg.Height > avatarMaxSide {
		writeErr(w, http.StatusBadRequest, "invalid_image", "avatar must be at most 4096×4096 px")
		return "", nil, false
	}
	return mime, data, true
}

func (d Deps) putAvatarFor(w http.ResponseWriter, r *http.Request, uid string) {
	mime, data, ok := readAvatar(w, r)
	if !ok {
		return
	}
	if err := d.Auth.SetAvatar(r.Context(), uid, mime, data); err != nil {
		d.acctErr(w, "avatar", err)
		return
	}
	u, err := d.Auth.UserByID(r.Context(), uid)
	if err != nil {
		d.acctErr(w, "avatar", err)
		return
	}
	d.Hub.Publish("user.updated", map[string]string{"id": uid})
	writeJSON(w, http.StatusOK, u)
}

// PUT /api/v1/me/avatar (raw image body)
func (d Deps) putMyAvatar(w http.ResponseWriter, r *http.Request) {
	dev, _ := DeviceFrom(r.Context())
	if dev.UserID == "" {
		writeErr(w, http.StatusForbidden, "account_required", "this device is not signed in to an account")
		return
	}
	d.putAvatarFor(w, r, dev.UserID)
}

// DELETE /api/v1/me/avatar
func (d Deps) deleteMyAvatar(w http.ResponseWriter, r *http.Request) {
	dev, _ := DeviceFrom(r.Context())
	if dev.UserID == "" {
		writeErr(w, http.StatusForbidden, "account_required", "this device is not signed in to an account")
		return
	}
	_ = d.Auth.DeleteAvatar(r.Context(), dev.UserID)
	w.WriteHeader(http.StatusNoContent)
}

// PUT/DELETE /api/v1/users/{id}/avatar (admin)
func (d Deps) putUserAvatar(w http.ResponseWriter, r *http.Request) {
	d.putAvatarFor(w, r, r.PathValue("id"))
}

func (d Deps) deleteUserAvatar(w http.ResponseWriter, r *http.Request) {
	if _, err := d.Auth.UserByID(r.Context(), r.PathValue("id")); err != nil {
		d.acctErr(w, "avatar", err)
		return
	}
	_ = d.Auth.DeleteAvatar(r.Context(), r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/v1/users/{id}/avatar — any authenticated device (household).
func (d Deps) getUserAvatar(w http.ResponseWriter, r *http.Request) {
	mime, data, at, err := d.Auth.Avatar(r.Context(), r.PathValue("id"))
	if errors.Is(err, auth.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not_found", "no avatar")
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "avatar", at, bytes.NewReader(data))
}
