package gateway

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
)

// PlaylistUseCases is implemented by *library.Scanner (via app.Library).
type PlaylistUseCases interface {
	ListPlaylists(ctx contextT) ([]library.Playlist, error)
	GetPlaylist(ctx contextT, id string, limit, offset int) (library.PlaylistPage, error)
	CreatePlaylist(ctx contextT, in library.PlaylistInput) (library.Playlist, error)
	UpdatePlaylist(ctx contextT, id string, in library.PlaylistInput) (library.Playlist, error)
	DeletePlaylist(ctx contextT, id string) error
	EditPlaylist(ctx contextT, id string, op library.PlaylistOp) (library.Playlist, error)
	ExportM3U(ctx contextT, id string, w io.Writer, relative bool) error
	ImportM3U(ctx contextT, name string, r io.Reader) (library.ImportResult, error)
	PlaylistTrackIDs(ctx contextT, id string) ([]string, error)
}

func (d Deps) plErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, library.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not_found", "playlist not found")
	case errors.Is(err, library.ErrBadInput), errors.Is(err, library.ErrBadRules):
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, library.ErrSmartReadOnly):
		writeErr(w, http.StatusConflict, "smart_playlist", err.Error())
	default:
		d.internal(w, "playlist", err)
	}
}

func (d Deps) plDone(w http.ResponseWriter, code int, p library.Playlist, err error) {
	if err != nil {
		d.plErr(w, err)
		return
	}
	d.Hub.Publish("playlist.updated", map[string]string{"id": p.ID})
	action := "updated"
	if code == http.StatusCreated {
		action = "created"
	}
	d.Hub.Publish("playlists.changed", map[string]any{"id": p.ID, "action": action, "updated_at": p.UpdatedAt})
	writeJSON(w, code, p)
}

func (d Deps) listPlaylists(w http.ResponseWriter, r *http.Request) {
	if d.listPlaylistScope(w, r, r.URL.Query().Get("scope")) {
		return
	}
	l, err := d.Playlists.ListPlaylists(r.Context())
	if err != nil {
		d.plErr(w, err)
		return
	}
	d.decoratePlaylists(r, l)
	writeJSON(w, http.StatusOK, map[string]any{"items": l, "total": len(l), "next_cursor": nil})
}

func (d Deps) getPlaylist(w http.ResponseWriter, r *http.Request) {
	p, err := d.Playlists.GetPlaylist(r.Context(), r.PathValue("id"), intQ(r, "limit"), intQ(r, "offset"))
	if err != nil {
		d.plErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (d Deps) createPlaylist(w http.ResponseWriter, r *http.Request) {
	var in library.PlaylistInput
	if err := decodeBodyMax(r, &in, 1<<20); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	p, err := d.Playlists.CreatePlaylist(r.Context(), in)
	d.plDone(w, http.StatusCreated, p, err)
}

func (d Deps) updatePlaylist(w http.ResponseWriter, r *http.Request) {
	var in library.PlaylistInput
	if err := decodeBody(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	p, err := d.Playlists.UpdatePlaylist(r.Context(), r.PathValue("id"), in)
	d.plDone(w, http.StatusOK, p, err)
}

func (d Deps) deletePlaylist(w http.ResponseWriter, r *http.Request) {
	if err := d.Playlists.DeletePlaylist(r.Context(), r.PathValue("id")); err != nil {
		d.plErr(w, err)
		return
	}
	d.Hub.Publish("playlist.deleted", map[string]string{"id": r.PathValue("id")})
	d.Hub.Publish("playlists.changed", map[string]any{"id": r.PathValue("id"), "action": "deleted"})
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) editPlaylist(w http.ResponseWriter, r *http.Request) {
	var op library.PlaylistOp
	if err := decodeBodyMax(r, &op, 1<<20); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	p, err := d.Playlists.EditPlaylist(r.Context(), r.PathValue("id"), op)
	d.plDone(w, http.StatusOK, p, err)
}

func (d Deps) exportM3U(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := d.Playlists.PlaylistTrackIDs(r.Context(), id); err != nil {
		d.plErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "audio/x-mpegurl; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+id+`.m3u8"`)
	if err := d.Playlists.ExportM3U(r.Context(), id, w, r.URL.Query().Get("paths") == "relative"); err != nil {
		d.Log.Warn("m3u export", "err", err)
	}
}

func (d Deps) importM3U(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		name = "Imported playlist"
	}
	res, err := d.Playlists.ImportM3U(r.Context(), name, http.MaxBytesReader(w, r.Body, 4<<20))
	if err != nil {
		d.plErr(w, err)
		return
	}
	d.Hub.Publish("playlist.updated", map[string]string{"id": res.Playlist.ID})
	writeJSON(w, http.StatusCreated, res)
}
