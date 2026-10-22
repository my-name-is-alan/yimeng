package gateway

import (
	"errors"
	"github.com/my-name-is-alan/yimeng/core/internal/runtimecfg"
	"io"
	"net/http"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
)

func (d Deps) getSplit(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, d.Library.SplitConfig())
}

// putSplit replaces the artist split policy and re-merges the library.
func (d Deps) putSplit(w http.ResponseWriter, r *http.Request) {
	var c library.SplitConfig
	if err := decodeBody(r, &c); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	got, err := d.Library.SetSplitConfig(r.Context(), c)
	switch {
	case errors.Is(err, library.ErrInvalidSettings):
		writeErr(w, http.StatusBadRequest, "invalid_settings", err.Error())
	case err != nil:
		d.internal(w, "save settings", err)
	default:
		writeJSON(w, http.StatusOK, got)
	}
}

// getServerSettings returns the runtime-editable server settings plus the
// selectable background-fill sources.
func (d Deps) getServerSettings(w http.ResponseWriter, r *http.Request) {
	if d.ServerSettings == nil {
		writeErr(w, http.StatusNotImplemented, "unavailable", "runtime settings not available")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": d.ServerSettings.Get(), "fill_source_ids": runtimecfg.FillSourceIDs})
}

// putServerSettings merges a partial update, persists it and applies it
// without a restart (watcher restart, MusicBrainz enricher restart, fill kick).
func (d Deps) putServerSettings(w http.ResponseWriter, r *http.Request) {
	if d.ServerSettings == nil {
		writeErr(w, http.StatusNotImplemented, "unavailable", "runtime settings not available")
		return
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, 8192))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	got, err := d.ServerSettings.Put(r.Context(), b)
	switch {
	case errors.Is(err, runtimecfg.ErrInvalid):
		writeErr(w, http.StatusBadRequest, "invalid_settings", err.Error())
	case err != nil:
		d.internal(w, "save server settings", err)
	default:
		d.Hub.Publish("settings.updated", map[string]any{"key": "server"})
		writeJSON(w, http.StatusOK, map[string]any{"settings": got, "fill_source_ids": runtimecfg.FillSourceIDs})
	}
}
