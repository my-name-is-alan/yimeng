package gateway

import (
	"errors"
	"net/http"

	"github.com/my-name-is-alan/yimeng/core/internal/auth"
	"github.com/my-name-is-alan/yimeng/core/internal/library"
)

// AdminLibrary covers root and metadata admin operations.
type AdminLibrary interface {
	Roots() []string
	SetLibraryRoots(ctx contextT, roots []string) error
	SetOverride(ctx contextT, typ, id string, patch map[string]any) error
	ClearOverride(ctx contextT, typ, id string) error
	GetOverride(ctx contextT, typ, id string) (map[string]any, error)
	MergeAlbums(ctx contextT, target string, sources []string) (library.Album, error)
	SplitAlbum(ctx contextT, source string, trackIDs []string, title, artistName, targetID string) (library.Album, error)
	GetAlbum(ctx contextT, id string) (library.Album, error)
	GetTrack(ctx contextT, id string) (library.Track, error)
	GetArtist(ctx contextT, id string) (library.Artist, error)
}

func (d Deps) patchDevice(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Name  *string `json:"name"`
		Admin *bool   `json:"admin"`
	}
	if err := decodeBody(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	id := r.PathValue("id")
	if b.Name != nil {
		if err := d.Auth.Rename(r.Context(), id, *b.Name); err != nil {
			d.deviceErr(w, err)
			return
		}
	}
	if b.Admin != nil {
		if err := d.Auth.SetAdmin(r.Context(), id, *b.Admin); err != nil {
			d.deviceErr(w, err)
			return
		}
	}
	devs, err := d.Auth.ListDevices(r.Context())
	if err != nil {
		d.internal(w, "devices", err)
		return
	}
	for _, d0 := range devs {
		if d0.ID == id {
			writeJSON(w, http.StatusOK, d0)
			return
		}
	}
	writeErr(w, http.StatusNotFound, "not_found", "device not found")
}

func (d Deps) deviceErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not_found", "device not found")
	case errors.Is(err, auth.ErrLastAdmin):
		writeErr(w, http.StatusConflict, "last_admin", err.Error())
	case errors.Is(err, auth.ErrBadName):
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
	default:
		d.internal(w, "device", err)
	}
}

func (d Deps) getRoots(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"roots": d.Admin.Roots()})
}

func (d Deps) putRoots(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Roots []string `json:"roots"`
	}
	if err := decodeBody(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if err := d.Admin.SetLibraryRoots(r.Context(), b.Roots); err != nil {
		if errors.Is(err, library.ErrBadInput) {
			writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		d.internal(w, "roots", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"roots": d.Admin.Roots()})
}

func (d Deps) putOverride(w http.ResponseWriter, r *http.Request) {
	var patch map[string]any
	if err := decodeBody(r, &patch); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	typ, id := r.PathValue("type"), r.PathValue("id")
	if err := d.Admin.SetOverride(r.Context(), typ, id, patch); err != nil {
		d.overrideErr(w, err)
		return
	}
	d.writeEntity(w, r, typ, id)
}

func (d Deps) deleteOverride(w http.ResponseWriter, r *http.Request) {
	if err := d.Admin.ClearOverride(r.Context(), r.PathValue("type"), r.PathValue("id")); err != nil {
		d.overrideErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) getOverride(w http.ResponseWriter, r *http.Request) {
	p, err := d.Admin.GetOverride(r.Context(), r.PathValue("type"), r.PathValue("id"))
	if err != nil {
		d.overrideErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"type": r.PathValue("type"), "id": r.PathValue("id"), "patch": p})
}

func (d Deps) overrideErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, library.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not_found", "not found")
	case errors.Is(err, library.ErrBadInput):
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
	default:
		d.internal(w, "override", err)
	}
}

func (d Deps) writeEntity(w http.ResponseWriter, r *http.Request, typ, id string) {
	var (
		v   any
		err error
	)
	switch typ {
	case "track":
		v, err = d.Admin.GetTrack(r.Context(), id)
	case "album":
		v, err = d.Admin.GetAlbum(r.Context(), id)
	case "artist":
		v, err = d.Admin.GetArtist(r.Context(), id)
	}
	if err != nil {
		d.overrideErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (d Deps) mergeAlbums(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Target  string   `json:"target"`
		Sources []string `json:"sources"`
	}
	if err := decodeBody(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	al, err := d.Admin.MergeAlbums(r.Context(), b.Target, b.Sources)
	if err != nil {
		d.overrideErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, al)
}

func (d Deps) splitAlbum(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Source     string   `json:"source"`
		TrackIDs   []string `json:"track_ids"`
		Title      string   `json:"title"`
		ArtistName string   `json:"artist_name"`
		TargetID   string   `json:"target_id"`
	}
	if err := decodeBody(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	al, err := d.Admin.SplitAlbum(r.Context(), b.Source, b.TrackIDs, b.Title, b.ArtistName, b.TargetID)
	if err != nil {
		d.overrideErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, al)
}
