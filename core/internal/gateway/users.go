package gateway

import (
	"errors"
	"net/http"

	"github.com/my-name-is-alan/yimeng/core/internal/auth"
)

// Web console accounts: first-run setup, username/password login, logout,
// password change and (admin) user management. A login issues the same kind
// of device token that pairing does, so every other route is unchanged.

func (d Deps) acctErr(w http.ResponseWriter, op string, err error) {
	switch {
	case errors.Is(err, auth.ErrRateLimited):
		w.Header().Set("Retry-After", "900")
		writeErr(w, http.StatusTooManyRequests, "rate_limited", "too many failed attempts; try again in 15 minutes")
	case errors.Is(err, auth.ErrBadCredentials):
		writeErr(w, http.StatusUnauthorized, "invalid_credentials", "用户名或密码错误")
	case errors.Is(err, auth.ErrWeakPassword):
		writeErr(w, http.StatusBadRequest, "weak_password", err.Error())
	case errors.Is(err, auth.ErrInvalidUser):
		writeErr(w, http.StatusBadRequest, "invalid_username", "username: 2-32 letters, digits, _ - .")
	case errors.Is(err, auth.ErrUserExists):
		writeErr(w, http.StatusConflict, "username_taken", "username already taken")
	case errors.Is(err, auth.ErrSetupDone):
		writeErr(w, http.StatusConflict, "setup_done", "an account already exists; sign in instead")
	case errors.Is(err, auth.ErrLastAdmin):
		writeErr(w, http.StatusConflict, "last_admin", err.Error())
	case errors.Is(err, auth.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not_found", "user not found")
	default:
		d.internal(w, op, err)
	}
}

func (d Deps) authStatus(w http.ResponseWriter, r *http.Request) {
	n, err := d.Auth.UserCount(r.Context())
	if err != nil {
		d.internal(w, "user count", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"needs_setup": n == 0})
}

type credBody struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	DeviceName string `json:"device_name,omitempty"`
	DeviceType string `json:"device_type,omitempty"`
}

func (d Deps) sessionReply(w http.ResponseWriter, status int, dev auth.Device, u auth.User, tok string) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, map[string]any{"token": tok, "device": dev, "user": u})
}

// authSetup creates the first (admin) account. Only works while no account exists.
func (d Deps) authSetup(w http.ResponseWriter, r *http.Request) {
	var b credBody
	if err := decodeBody(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	u, err := d.Auth.CreateUser(r.Context(), b.Username, b.Password, auth.RoleAdmin, true)
	if err != nil {
		d.acctErr(w, "setup", err)
		return
	}
	d.Log.Info("first admin account created", "username", u.Username)
	dev, u, tok, err := d.Auth.Login(r.Context(), clientIP(r), b.Username, b.Password, b.DeviceName, b.DeviceType)
	if err != nil {
		d.acctErr(w, "setup login", err)
		return
	}
	d.sessionReply(w, http.StatusCreated, dev, u, tok)
}

func (d Deps) authLogin(w http.ResponseWriter, r *http.Request) {
	var b credBody
	if err := decodeBody(r, &b); err != nil || b.Username == "" || b.Password == "" || len(b.Username) > 64 || len(b.Password) > 256 {
		writeErr(w, http.StatusBadRequest, "invalid_body", "username and password are required")
		return
	}
	dev, u, tok, err := d.Auth.Login(r.Context(), clientIP(r), b.Username, b.Password, b.DeviceName, b.DeviceType)
	if err != nil {
		if errors.Is(err, auth.ErrBadCredentials) || errors.Is(err, auth.ErrRateLimited) {
			d.Log.Warn("web login failed", "ip", clientIP(r), "reason", err.Error())
		}
		d.acctErr(w, "login", err)
		return
	}
	d.Log.Info("web login", "user", u.Username, "device", dev.ID)
	d.sessionReply(w, http.StatusOK, dev, u, tok)
}

// authLogout revokes the calling session's token.
func (d Deps) authLogout(w http.ResponseWriter, r *http.Request) {
	dev, _ := DeviceFrom(r.Context())
	if err := d.Auth.Revoke(r.Context(), dev.ID); err != nil && !errors.Is(err, auth.ErrNotFound) {
		d.internal(w, "logout", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) authPassword(w http.ResponseWriter, r *http.Request) {
	dev, _ := DeviceFrom(r.Context())
	if dev.UserID == "" {
		writeErr(w, http.StatusConflict, "not_a_user_session", "this device was paired with a code; sign in with a username to change a password")
		return
	}
	var b struct {
		Old string `json:"old_password"`
		New string `json:"new_password"`
	}
	if err := decodeBody(r, &b); err != nil || b.New == "" {
		writeErr(w, http.StatusBadRequest, "invalid_body", "old_password and new_password are required")
		return
	}
	if err := d.Auth.ChangePassword(r.Context(), dev.UserID, b.Old, b.New, dev.ID); err != nil {
		if errors.Is(err, auth.ErrBadCredentials) {
			writeErr(w, http.StatusForbidden, "invalid_credentials", "当前密码错误")
			return
		}
		d.acctErr(w, "change password", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) listUsers(w http.ResponseWriter, r *http.Request) {
	us, err := d.Auth.ListUsers(r.Context())
	if err != nil {
		d.internal(w, "list users", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": us})
}

func (d Deps) createUser(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := decodeBody(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if b.Role == "" {
		b.Role = auth.RoleUser
	}
	u, err := d.Auth.CreateUser(r.Context(), b.Username, b.Password, b.Role, false)
	if err != nil {
		d.acctErr(w, "create user", err)
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

func (d Deps) patchUser(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Role     *string `json:"role"`
		Password *string `json:"password"`
	}
	if err := decodeBody(r, &b); err != nil || (b.Role == nil && b.Password == nil) {
		writeErr(w, http.StatusBadRequest, "invalid_body", "role and/or password required")
		return
	}
	id := r.PathValue("id")
	if b.Role != nil {
		if err := d.Auth.SetRole(r.Context(), id, *b.Role); err != nil {
			d.acctErr(w, "set role", err)
			return
		}
	}
	if b.Password != nil {
		dev, _ := DeviceFrom(r.Context())
		if err := d.Auth.SetPassword(r.Context(), id, *b.Password, dev.ID); err != nil {
			d.acctErr(w, "set password", err)
			return
		}
	}
	u, err := d.Auth.UserByID(r.Context(), id)
	if err != nil {
		d.acctErr(w, "get user", err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (d Deps) deleteUser(w http.ResponseWriter, r *http.Request) {
	if err := d.Auth.DeleteUser(r.Context(), r.PathValue("id")); err != nil {
		d.acctErr(w, "delete user", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
