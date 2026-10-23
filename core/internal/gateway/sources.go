package gateway

import (
	"errors"
	"net/http"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/playback"
	"github.com/my-name-is-alan/yimeng/core/internal/vault"
)

type sourceDTO struct {
	ID           string                     `json:"id"`
	Health       playback.SourceHealth      `json:"health"`
	LoginMethods []playback.LoginMethodInfo `json:"login_methods"`
	Account      *playback.AccountInfo      `json:"account,omitempty"`
	Linked       bool                       `json:"linked"`
}

func (d Deps) listSources(w http.ResponseWriter, r *http.Request) {
	items := []sourceDTO{}
	if d.Sources != nil {
		for _, s := range d.Sources.All() {
			items = append(items, d.sourceDTO(r, s))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (d Deps) getSource(w http.ResponseWriter, r *http.Request) {
	s, ok := d.source(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "not_found", "unknown source")
		return
	}
	writeJSON(w, http.StatusOK, d.sourceDTO(r, s))
}

func (d Deps) sourceDTO(r *http.Request, s playback.StreamSource) sourceDTO {
	dto := sourceDTO{
		ID: s.ID(), Health: s.Health(r.Context()), LoginMethods: s.LoginMethods(),
	}
	dev, _ := DeviceFrom(r.Context())
	if d.Accounts != nil && !dev.Admin {
		return dto
	}
	if info, err := s.Account(r.Context()); err == nil {
		dto.Account = &info
		dto.Linked = true
	}
	return dto
}

func (d Deps) source(id string) (playback.StreamSource, bool) {
	if d.Sources == nil {
		return nil, false
	}
	return d.Sources.Get(id)
}

func (d Deps) loginSource(w http.ResponseWriter, r *http.Request) {
	s, ok := d.source(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "not_found", "unknown source")
		return
	}
	var in playback.LoginInput
	if err := decodeBody(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	info, err := s.Login(r.Context(), in)
	if err != nil {
		d.sourceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (d Deps) startQR(w http.ResponseWriter, r *http.Request) {
	s, ok := d.source(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "not_found", "unknown source")
		return
	}
	ch, err := s.StartQRLogin(r.Context())
	if err != nil {
		d.sourceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": ch.ID, "image_url": ch.ImageURL, "expires_at": ch.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

func (d Deps) pollQR(w http.ResponseWriter, r *http.Request) {
	s, ok := d.source(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "not_found", "unknown source")
		return
	}
	st, err := s.PollQRLogin(r.Context(), r.PathValue("challenge"))
	if err != nil {
		d.sourceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": st})
}

func (d Deps) logoutSource(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s, ok := d.source(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "not_found", "unknown source")
		return
	}
	_ = s.Logout(r.Context())
	if d.Vault != nil {
		_ = d.Vault.Delete(r.Context(), id)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) syncSource(w http.ResponseWriter, r *http.Request) {
	s, ok := d.source(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "not_found", "unknown source")
		return
	}
	page, err := s.SyncLibrary(r.Context(), r.URL.Query().Get("cursor"))
	if err != nil {
		d.sourceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (d Deps) sourceErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, playback.ErrNotSupported):
		writeErr(w, http.StatusNotImplemented, "not_supported", err.Error())
	case errors.Is(err, playback.ErrNeedsLogin), errors.Is(err, vault.ErrNotFound):
		writeErr(w, http.StatusUnauthorized, "needs_login", err.Error())
	case errors.Is(err, playback.ErrAuthExpired), errors.Is(err, vault.ErrNeedsReauth):
		writeErr(w, http.StatusUnauthorized, "auth_expired", err.Error())
	default:
		d.internal(w, "source", err)
	}
}
